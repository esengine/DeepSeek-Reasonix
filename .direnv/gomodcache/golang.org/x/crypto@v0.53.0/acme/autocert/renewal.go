// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autocert

import (
	"context"
	"crypto"
	"sync"
	"time"
)

// domainRenewal tracks the state used by the periodic timers
// renewing a single domain's cert.
type domainRenewal struct {
	m   *Manager
	ck  certKey
	key crypto.Signer

	timerMu    sync.Mutex
	timer      *time.Timer
	timerClose chan struct{} // if non-nil, renew closes this channel (and nils out the timer fields) instead of running
}

// start starts a cert renewal timer at the time
// defined by the certificate expiration time exp.
//
// If the timer is already started, calling start is a noop.
func (dr *domainRenewal) start(notBefore, notAfter time.Time) {
	dr.timerMu.Lock()
	defer dr.timerMu.Unlock()
	if dr.timer != nil {
		return
	}
	dr.timer = time.AfterFunc(dr.next(notBefore, notAfter), dr.renew)
}

// stop stops the cert renewal timer and waits for any in-flight calls to renew
// to complete. If the timer is already stopped, calling stop is a noop.
func (dr *domainRenewal) stop() {
	dr.timerMu.Lock()
	defer dr.timerMu.Unlock()
	for {
		if dr.timer == nil {
			return
		}
		if dr.timer.Stop() {
			dr.timer = nil
			return
		} else {
			// dr.timer fired, and we acquired dr.timerMu before the renew callback did.
			// (We know this because otherwise the renew callback would have reset dr.timer!)
			timerClose := make(chan struct{})
			dr.timerClose = timerClose
			dr.timerMu.Unlock()
			<-timerClose
			dr.timerMu.Lock()
		}
	}
}

// renew is called periodically by a timer.
// The first renew call is kicked off by dr.start.
func (dr *domainRenewal) renew() {
	dr.timerMu.Lock()
	defer dr.timerMu.Unlock()
	if dr.timerClose != nil {
		close(dr.timerClose)
		dr.timer, dr.timerClose = nil, nil
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// TODO: rotate dr.key at some point?
	next, err := dr.do(ctx)
	if err != nil {
		next = time.Hour / 2
		next += time.Duration(pseudoRand.int63n(int64(next)))
	}
	testDidRenewLoop(next, err)
	dr.timer = time.AfterFunc(next, dr.renew)
}

// updateState locks and replaces the relevant Manager.state item with the given
// state. It additionally updates dr.key with the given state's key.
func (dr *domainRenewal) updateState(state *certState) {
	dr.m.stateMu.Lock()
	defer dr.m.stateMu.Unlock()
	dr.key = state.key
	dr.m.state[dr.ck] = state
}

// do is similar to Manager.createCert but it doesn't lock a Manager.state item.
// Instead, it requests a new certificate independently and, upon success,
// replaces dr.m.state item with a new one and updates cache for the given domain.
//
// It may lock and update the Manager.state if the expiration date of the currently
// cached cert is far enough in the future.
//
// The returned value is a time interval after which the renewal should occur again.
func (dr *domainRenewal) do(ctx context.Context) (time.Duration, error) {
	// a race is likely unavoidable in a distributed environment
	// but we try nonetheless
	if tlscert, err := dr.m.cacheGet(ctx, dr.ck); err == nil {
		next := dr.next(tlscert.Leaf.NotBefore, tlscert.Leaf.NotAfter)
		if next > 0 {
			signer, ok := tlscert.PrivateKey.(crypto.Signer)
			if ok {
				state := &certState{
					key:  signer,
					cert: tlscert.Certificate,
					leaf: tlscert.Leaf,
				}
				dr.updateState(state)
				return next, nil
			}
		}
	}

	der, leaf, err := dr.m.authorizedCert(ctx, dr.key, dr.ck)
	if err != nil {
		return 0, err
	}
	state := &certState{
		key:  dr.key,
		cert: der,
		leaf: leaf,
	}
	tlscert, err := state.tlscert()
	if err != nil {
		return 0, err
	}
	if err := dr.m.cachePut(ctx, dr.ck, tlscert); err != nil {
		return 0, err
	}
	dr.updateState(state)
	return dr.next(leaf.NotBefore, leaf.NotAfter), nil
}

// next returns the wait time before the next renewal should start.
// If manager.RenewBefore is set, it uses that capped at 30 days,
// otherwise it uses a default of 1/3 of the cert lifetime.
// It builds in a jitter of 10% of the renew threshold, capped at 1 hour.
func (dr *domainRenewal) next(notBefore, notAfter time.Time) time.Duration {
	threshold := min(notAfter.Sub(notBefore)/3, 30*24*time.Hour)
	if dr.m.RenewBefore > 0 {
		threshold = min(dr.m.RenewBefore, 30*24*time.Hour)
	}
	maxJitter := min(threshold/10, time.Hour)
	jitter := pseudoRand.int63n(int64(maxJitter))
	renewAt := notAfter.Add(-(threshold - time.Duration(jitter)))
	renewWait := renewAt.Sub(dr.m.now())
	return max(0, renewWait)
}

var testDidRenewLoop = func(next time.Duration, err error) {}
