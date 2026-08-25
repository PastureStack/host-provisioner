module github.com/PastureStack/host-provisioner

go 1.27.0

require (
	github.com/distribution/reference v0.6.0
	github.com/docker/machine v0.16.2-gitlab.52
	github.com/fsouza/go-dockerclient v1.13.3
	github.com/rancher/event-subscriber v0.0.0
	github.com/rancher/go-rancher v0.0.0
	github.com/sirupsen/logrus v1.10.1
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/moby/go-archive v0.3.3 // indirect
	github.com/moby/moby/api v1.55.0 // indirect
	github.com/moby/moby/client v0.5.1 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/sequential v0.7.0 // indirect
	github.com/moby/sys/user v0.4.1 // indirect
	github.com/moby/sys/userns v0.2.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
)

replace github.com/docker/machine => gitlab.com/gitlab-org/ci-cd/docker-machine v0.16.2-gitlab.52

replace github.com/rancher/event-subscriber => ./third_party/event-subscriber

replace github.com/rancher/go-rancher => ./third_party/go-rancher
