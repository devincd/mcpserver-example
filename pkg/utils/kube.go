package utils

import (
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)

// GetKubeRestConfig 根据配置获取连接Kubernetes需要的配置
func GetKubeRestConfig(kc string) (*rest.Config, error) {
	kubeconfig := kc
	if kubeconfig == "" {
		kubeconfig = filepath.Join(homedir.HomeDir(), ".kube", "config")
		if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
			kubeconfig = ""
		}
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}
