module github.com/rancher/event-subscriber

go 1.26.0

toolchain go1.27.0

require (
	github.com/gorilla/websocket v1.5.3
	github.com/rancher/go-rancher v0.0.0
	github.com/sirupsen/logrus v1.10.2
)

require golang.org/x/sys v0.47.0 // indirect

replace github.com/rancher/go-rancher => ../go-rancher
