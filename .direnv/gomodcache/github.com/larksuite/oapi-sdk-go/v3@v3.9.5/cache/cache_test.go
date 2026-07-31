package larkcache

import (
	"fmt"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	cache := New(1 * time.Second)
	cache.Set("a", "a", 3*time.Second)
	time.Sleep(5 * time.Second)
	val := cache.Get("a")
	fmt.Println(val)
}
