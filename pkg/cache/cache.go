package cache

import "sync"

type Cache struct {
	data map[string]interface{}
	lock *sync.RWMutex
}

func NewCache() *Cache {
	return &Cache{
		data: make(map[string]interface{}),
		lock: &sync.RWMutex{},
	}
}

// Set 设置缓存数据，如果key存在覆盖掉
func (c *Cache) Set(key string, value interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.data[key] = value
	return
}

// Get 获取缓存数据
func (c *Cache) Get(key string) (interface{}, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	val, ok := c.data[key]
	return val, ok
}

// Delete 删除缓存数据
func (c *Cache) Delete(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.data, key)
	return
}
