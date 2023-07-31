package cache

import "testing"

func Test_Cache(t *testing.T) {
	c := NewCache()

	c.Set("name", "cd")
	if val, ok := c.Get("name"); ok {
		if val == "cd" {
			t.Log("success")
			return
		}
	}
	t.Error("error")
}
