package cmd

import (
	"flag"
	"k8s.io/klog/v2"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// WaitSignal awaits for SIGINT or SIGTERM and closes the channel
func WaitSignal(stop chan struct{}) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	klog.InfoS("Receive the signal", "signal", sig.String())
	close(stop)
	// do something else
}

// WaitSignalFunc awaits for SIGINT or SIGTERM and calls the cancel function
func WaitSignalFunc(cancel func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	klog.InfoS("Receive the signal", "signal", sig.String())
	cancel()
	// do something else
}

// AddFlags adds all command line flags to the given command
func AddFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

// PrintFlags logs the flags in the flagset
func PrintFlags(flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		klog.Infof("FLAG: --%s=%q", f.Name, f.Value)
	})
}
