package observation

import (
	"encoding/json"
	"fmt"
	"path"

	"reasonix/internal/state/trustedstate"
)

// Changed lists the slash-separated paths, relative to the root, that differ
// between a and b: added, removed, or re-stamped. It descends only into
// subtrees whose digests differ, and stops listing after limit paths while
// still counting them.
func (o *Observer) Changed(a, b Snapshot, limit int) (paths []string, count int, err error) {
	if !a.Complete || !b.Complete || a.Policy != b.Policy {
		return nil, 0, ErrIncomparable
	}
	d := differ{o: o, limit: limit}
	if err := d.diff(a.Root, b.Root, ""); err != nil {
		return nil, 0, err
	}
	return d.paths, d.count, nil
}

type differ struct {
	o     *Observer
	limit int
	paths []string
	count int
}

func (d *differ) note(p string) {
	d.count++
	if len(d.paths) < d.limit {
		d.paths = append(d.paths, p)
	}
}

func (d *differ) load(ref string) ([]node, error) {
	if ref == "" {
		return nil, nil
	}
	b, err := d.o.store.Object(trustedstate.Digest(ref))
	if err != nil {
		return nil, err
	}
	var nodes []node
	if err := json.Unmarshal(b, &nodes); err != nil {
		return nil, fmt.Errorf("%w: tree node %s does not parse", trustedstate.ErrTampered, ref)
	}
	return nodes, nil
}

func (d *differ) diff(a, b, prefix string) error {
	if a == b {
		return nil
	}
	left, err := d.load(a)
	if err != nil {
		return err
	}
	right, err := d.load(b)
	if err != nil {
		return err
	}
	i, j := 0, 0
	for i < len(left) || j < len(right) {
		switch {
		case j == len(right) || (i < len(left) && left[i].Name < right[j].Name):
			if err := d.whole(left[i], prefix); err != nil {
				return err
			}
			i++
		case i == len(left) || right[j].Name < left[i].Name:
			if err := d.whole(right[j], prefix); err != nil {
				return err
			}
			j++
		default:
			if err := d.pair(left[i], right[j], prefix); err != nil {
				return err
			}
			i, j = i+1, j+1
		}
	}
	return nil
}

func (d *differ) pair(l, r node, prefix string) error {
	p := path.Join(prefix, l.Name)
	if l.Kind == "d" && r.Kind == "d" {
		if l.Mode != r.Mode {
			d.note(p)
		}
		return d.diff(l.Ref, r.Ref, p)
	}
	if l.Kind == r.Kind && l.Mode == r.Mode && l.Ref == r.Ref && sameStamp(l.Stamp, r.Stamp) {
		return nil
	}
	switch {
	case l.Kind == "d":
		return d.whole(l, prefix)
	case r.Kind == "d":
		return d.whole(r, prefix)
	}
	d.note(p)
	return nil
}

// whole notes an entry present on one side only, and everything beneath it.
func (d *differ) whole(n node, prefix string) error {
	p := path.Join(prefix, n.Name)
	d.note(p)
	if n.Kind != "d" {
		return nil
	}
	children, err := d.load(n.Ref)
	if err != nil {
		return err
	}
	for _, c := range children {
		if err := d.whole(c, p); err != nil {
			return err
		}
	}
	return nil
}

func sameStamp(a, b *stamp) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
