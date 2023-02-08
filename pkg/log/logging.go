package log

import (
	"flag"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"
)

// InitLogging function must be called at the first line of program
func InitLogging() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	path := filepath.Dir(exe)
	log_dir := filepath.Join(path, "..", "log")
	if _, err := os.Stat(log_dir); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(log_dir, 0750); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	osArgs := os.Args
	os.Args = []string{osArgs[0], "--log_dir", log_dir, "--logtostderr=false", "--alsologtostderr=false", "--one_output=false"}
	klog.InitFlags(nil)
	flag.Parse()
	os.Args = osArgs
	return nil
}
