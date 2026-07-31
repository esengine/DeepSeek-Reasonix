package larkcache

import (
	"runtime"
	"sync"
	"time"
)

func New(clearInterval time.Duration) *Cache {
	cache := &Cache{}
	cache.cron = newCron(cache.clear, clearInterval)
	runtime.SetFinalizer(cache, stopCron)

	return cache
}

type Cache struct {
	m    sync.Map
	cron *cron
}

type elem struct {
	val    interface{}
	expire time.Time
}

type cron struct {
	cmd      func()
	interval time.Duration
	stop     chan bool
}

func (c *cron) start() {
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C:
			c.cmd()
		case <-c.stop:
			ticker.Stop()
			return
		}
	}
}

func newCron(cmd func(), interval time.Duration) *cron {
	cron := &cron{
		cmd:      cmd,
		interval: interval,
		stop:     make(chan bool),
	}

	go cron.start()

	return cron
}

func stopCron(c *Cache) {
	c.cron.stop <- true
}

func (c *Cache) Get(key string) interface{} {
	if val, ok := c.m.Load(key); ok {
		elem := val.(*elem)
		if elem.expire.After(time.Now()) {
			return elem.val
		}
	}
	return nil
}

func (c *Cache) Set(key string, value interface{}, expire time.Duration) {
	expireTime := time.Now().Add(expire)
	c.m.Store(key, &elem{
		val:    value,
		expire: expireTime,
	})
}

func (c *Cache) clear() {
	c.m.Range(func(k, v interface{}) bool {
		elem := v.(*elem)
		if elem.expire.Before(time.Now()) {
			c.m.Delete(k)
		}
		return true
	})
}
