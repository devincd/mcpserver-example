package controller

import (
	"fmt"

	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type Controller struct {
	clientset       istioclient.Interface
	informerFactory istioinformers.SharedInformerFactory
	istioInformers  map[resource.Schema]istioinformers.GenericInformer

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	Workqueue workqueue.RateLimitingInterface
}

type ActionType int

const (
	ActionAdd ActionType = iota
	ActionUpdate
	ActionDelete
)

type ResourceEvent struct {
	GVK    string
	Action ActionType
}

func NewIstioController(clientset istioclient.Interface, informerFactory istioinformers.SharedInformerFactory) (*Controller, error) {
	c := &Controller{
		clientset:       clientset,
		informerFactory: informerFactory,
		istioInformers:  make(map[resource.Schema]istioinformers.GenericInformer),

		Workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Istio-Controller"),
	}
	// (1)将istio所有资源的informer都存在下来
	for _, sch := range collections.Pilot.All() {
		gvr := sch.Resource().GroupVersionResource()
		genericInformer, err := informerFactory.ForResource(gvr)
		if err != nil {
			return c, err
		}
		c.istioInformers[sch.Resource()] = genericInformer
	}
	// (2)注册istio各种资源的事件处理器
	for res, genericInformer := range c.istioInformers {
		// 防止指针类型最后只取最后一个地址
		resParam := res
		genericInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				klog.InfoS("Add istio events", "gvk", resParam.GroupVersionKind().String())
				c.enqueue(resParam.GroupVersionKind().String())
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				klog.InfoS("Update istio events", "gvk", resParam.GroupVersionKind().String())
				c.enqueue(resParam.GroupVersionKind().String())
			},
			DeleteFunc: func(obj interface{}) {
				klog.InfoS("Delete istio events", "gvk", resParam.GroupVersionKind().String())
				c.enqueue(resParam.GroupVersionKind().String())
			},
		})
	}
	return c, nil
}

func (c *Controller) enqueue(gvk string) {
	c.Workqueue.Add(gvk)
}

// WaitForCacheSync 确保资源都存放到缓存中去
func (c *Controller) WaitForCacheSync(stop <-chan struct{}) bool {
	syncedList := make([]cache.InformerSynced, 0)
	for _, genericInformer := range c.istioInformers {
		syncedList = append(syncedList, genericInformer.Informer().HasSynced)
	}
	return cache.WaitForCacheSync(stop, syncedList...)
}

func (c *Controller) Run() error {
	return nil
}

func (c *Controller) List(gvk string) ([]runtime.Object, error) {
	for res, informer := range c.istioInformers {
		if res.GroupVersionKind().String() == gvk {
			lister := informer.Lister()
			ret, err := lister.List(labels.Everything())
			return ret, err
		}
	}
	return nil, fmt.Errorf("can't find the lister according to the gvk: %s", gvk)
}
