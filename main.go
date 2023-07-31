package main

import (
	"os"

	"devincd.io/mcpserver-example/app"
	"k8s.io/klog/v2"
)

func main() {
	command := app.NewMCPServerCommand()
	if err := command.Execute(); err != nil {
		klog.ErrorS(err, "execute the mcpserver command error")
		os.Exit(-1)
	}
}
