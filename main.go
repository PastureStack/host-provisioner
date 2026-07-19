package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/PastureStack/host-provisioner/dynamic"
	"github.com/PastureStack/host-provisioner/handlers"
	"github.com/PastureStack/host-provisioner/logging"
	"github.com/rancher/event-subscriber/events"
)

var (
	GITCOMMIT = "HEAD"
)

var logger = logging.Logger()
var operatorLocale = "en-US"

func main() {
	processCmdLineFlags()

	logger.WithField("gitcommit", GITCOMMIT).Info(operatorMessage(operatorLocale, "start"))

	apiURL := environmentValue("PLATFORM_URL", "CATTLE_URL")
	accessKey := environmentValue("PLATFORM_ACCESS_KEY", "CATTLE_ACCESS_KEY")
	secretKey := environmentValue("PLATFORM_SECRET_KEY", "CATTLE_SECRET_KEY")

	ready := make(chan bool, 2)
	done := make(chan error)

	go func() {
		eventHandlers := map[string]events.EventHandler{
			"machinedriver.reactivate": handlers.ActivateDriver,
			"machinedriver.activate":   handlers.ActivateDriver,
			"machinedriver.update":     handlers.ActivateDriver,
			"machinedriver.error":      handlers.ErrorDriver,
			"machinedriver.deactivate": handlers.DeactivateDriver,
			"machinedriver.remove":     handlers.RemoveDriver,
			"ping":                     handlers.PingNoOp,
		}

		router, err := events.NewEventRouter("hostProvisioner-machine", 2000, apiURL, accessKey, secretKey,
			nil, eventHandlers, "machineDriver", 250, events.DefaultPingConfig)
		if err == nil {
			err = router.Start(ready)
		}
		done <- err
	}()

	go func() {
		eventHandlers := map[string]events.EventHandler{
			"physicalhost.create":    handlers.CreateMachine,
			"physicalhost.bootstrap": handlers.ActivateMachine,
			"physicalhost.remove":    handlers.PurgeMachine,
			"ping":                   handlers.PingNoOp,
		}

		router, err := events.NewEventRouter("hostProvisioner", 2000, apiURL, accessKey, secretKey,
			nil, eventHandlers, "physicalhost", 250, events.DefaultPingConfig)
		if err == nil {
			err = router.Start(ready)
		}
		done <- err
	}()

	go func() {
		// Can not remove this as nothing will delete the handler entries
		eventHandlers := map[string]events.EventHandler{
			"ping": handlers.PingNoOp,
		}

		router, err := events.NewEventRouter("hostProvisioner-agent", 2000, apiURL, accessKey, secretKey,
			nil, eventHandlers, "agent", 5, events.DefaultPingConfig)
		if err == nil {
			err = router.Start(ready)
		}
		done <- err
	}()

	go func() {
		logger.Infof("Waiting for handler registration (1/2)")
		<-ready
		logger.Infof("Waiting for handler registration (2/2)")
		<-ready
		if err := dynamic.ReactivateOldDrivers(); err != nil {
			logger.Fatalf("Error reactivating old drivers: %v", err)
		}
		if err := dynamic.DownloadAllDrivers(); err != nil {
			logger.Fatalf("Error updating drivers: %v", err)
		}
	}()

	err := <-done
	if err == nil {
		logger.Info(operatorMessage(operatorLocale, "exit"))
	} else {
		logger.Fatalf("Exiting host-provisioner: %v", err)
	}
}

func processCmdLineFlags() {
	// Define command line flags
	version := flag.Bool("v", false, "read the version of the host-provisioner")
	flag.StringVar(&operatorLocale, "locale", localeFromEnvironment(), "operator message locale: en-US or zh-TW")
	flag.Parse()
	if operatorLocale != "en-US" && operatorLocale != "zh-TW" {
		logger.Fatalf("unsupported locale %q; use en-US or zh-TW", operatorLocale)
	}
	if *version {
		fmt.Printf("host-provisioner\t gitcommit=%s\n", GITCOMMIT)
		os.Exit(0)
	}
}

func environmentValue(preferred, legacy string) string {
	if value := os.Getenv(preferred); value != "" {
		return value
	}
	return os.Getenv(legacy)
}

func localeFromEnvironment() string {
	if locale := os.Getenv("PASTURESTACK_LOCALE"); locale != "" {
		return locale
	}
	return "en-US"
}

func operatorMessage(locale, key string) string {
	messages := map[string]map[string]string{
		"en-US": {"start": "Starting PastureStack host provisioner", "exit": "PastureStack host provisioner stopped"},
		"zh-TW": {"start": "正在啟動 PastureStack 主機佈建服務", "exit": "PastureStack 主機佈建服務已停止"},
	}
	return messages[locale][key]
}
