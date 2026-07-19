package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/pkg/errors"
	"github.com/rancher/event-subscriber/events"
	client "github.com/rancher/go-rancher/v2"
	"github.com/sirupsen/logrus"
)

var lock = sync.Mutex{}

const (
	machineDirEnvKey    = "MACHINE_STORAGE_PATH="
	machineCmd          = "docker-machine"
	defaultPlatformHome = "/var/lib/pasturestack"
)

type machineInfo struct {
	// full path including jailDir - jailDir + /var/lib/cattle/machine/machines/{ExternalId}
	fullMachinePath string
	// path to base of jail
	jailDir string
}

func PingNoOp(event *events.Event, apiClient *client.RancherClient) error {
	// No-op ping handler
	return nil
}

func buildBaseMachineDir(m *client.Machine) (string, error) {
	identifier, err := machineDirectoryIdentifier(m)
	if err != nil {
		return "", err
	}

	workDir, err := trustedWorkDir()
	if err != nil {
		return "", err
	}
	machinesDir := filepath.Join(workDir, "machines")
	if err := os.MkdirAll(machinesDir, 0740); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(machinesDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := root.MkdirAll(identifier, 0740); err != nil {
		return "", err
	}
	return filepath.Join(machinesDir, identifier), nil
}

func getWorkDir() string {
	workDir := os.Getenv("MACHINE_WORK_DIR")
	if workDir == "" {
		workDir = os.Getenv("PASTURESTACK_HOME")
	}
	if workDir == "" {
		workDir = os.Getenv("CATTLE_HOME")
	}
	if workDir == "" {
		workDir = defaultPlatformHome
	}
	return filepath.Join(workDir, "machine")
}

// trustedWorkDir resolves the operator-controlled storage root before any
// control-plane identifier is appended to it. The root may be configured as
// either an absolute path or a relative path, but is always made absolute and
// cleaned at this trust boundary.
func trustedWorkDir() (string, error) {
	workDir, err := filepath.Abs(getWorkDir())
	if err != nil {
		return "", fmt.Errorf("resolve machine work directory: %w", err)
	}
	workDir = filepath.Clean(workDir)
	if strings.IndexByte(workDir, 0) >= 0 || !strings.HasPrefix(workDir, string(filepath.Separator)) || !filepath.IsAbs(workDir) {
		return "", fmt.Errorf("machine work directory is not an absolute storage path")
	}
	volumeRoot := filepath.Clean(filepath.VolumeName(workDir) + string(filepath.Separator))
	if workDir == volumeRoot {
		return "", fmt.Errorf("machine work directory must not be a volume root")
	}
	return workDir, nil
}

// machinePathIdentifier accepts the historical opaque identifiers emitted by
// the control plane when they are already safe path segments. For unusual but
// legitimate legacy identifiers, it derives a stable, non-reversible key
// instead of rejecting the machine or allowing path traversal.
func machinePathIdentifier(externalID string) (string, error) {
	if strings.IndexByte(externalID, 0) >= 0 {
		return "", fmt.Errorf("machine external identifier contains a NUL byte")
	}
	hasControlCharacter := false
	for _, character := range externalID {
		if unicode.IsControl(character) {
			hasControlCharacter = true
			break
		}
	}
	if len(externalID) <= 255 && !hasControlCharacter && filepath.IsLocal(externalID) && filepath.Base(externalID) == externalID &&
		externalID != "." && !strings.ContainsAny(externalID, `/\`) {
		return externalID, nil
	}
	if externalID == "" {
		return "", fmt.Errorf("machine external identifier is empty")
	}
	digest := sha256.Sum256([]byte(externalID))
	return fmt.Sprintf("legacy-%x", digest[:16]), nil
}

func machineDirectoryIdentifier(machine *client.Machine) (string, error) {
	identifier := machine.ExternalId
	if identifier == "" {
		identifier = machine.Id
	}
	return machinePathIdentifier(identifier)
}

func machineStorageName(machine *client.Machine) (string, error) {
	return machineCommandName(machine)
}

func machineCommandName(machine *client.Machine) (string, error) {
	name := machine.Name
	if name == "" {
		name = machine.ExternalId
	}
	if name == "" {
		name = machine.Id
	}
	if name == "" {
		return "", fmt.Errorf("machine name is empty")
	}
	if len(name) > 255 || name == "." || name == ".." || strings.HasPrefix(name, "-") ||
		!filepath.IsLocal(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("machine name %q is not a safe local name", name)
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("machine name contains a control character")
		}
	}
	return name, nil
}

var publishReply = func(reply *client.Publish, apiClient *client.RancherClient) error {
	_, err := apiClient.Publish.Create(reply)
	return err
}

var publishTransitioningReply = func(msg string, event *events.Event, apiClient *client.RancherClient) {
	// Since this is only updating the msg for the state transition, we will ignore errors here
	replyT := newReply(event)
	replyT.Transitioning = "yes"
	replyT.TransitioningMessage = msg
	publishReply(replyT, apiClient)
}

func republishTransitioningReply(publishChan <-chan string, event *events.Event, apiClient *client.RancherClient) {
	// Preserve this retry because the compatibility control plane can drop a transition message
	// has not been updated for a period of time, it can no longer be updated.  For now, to deal with this
	// we will simply republish transitioning messages until the next one is added.
	// Because this ticker is going to republish every X seconds, it's will most likely republish a message sooner
	// In all likelihood, we will remove this method later.
	defaultWaitTime := time.Second * 15
	ticker := time.NewTicker(defaultWaitTime)
	var lastMsg string
	for {
		select {
		case msg, more := <-publishChan:
			if !more {
				ticker.Stop()
				return
			}
			lastMsg = msg
			publishTransitioningReply(lastMsg, event, apiClient)

		case <-ticker.C:
			//republish last message
			if lastMsg != "" {
				publishTransitioningReply(lastMsg, event, apiClient)
			}
		}
	}
}

var getMachine = func(id string, apiClient *client.RancherClient) (*client.Machine, error) {
	m, err := apiClient.Machine.ById(id)
	if err != nil || m == nil {
		return nil, err
	}

	host, err := getHost(m, apiClient)
	if err != nil || host == nil {
		return m, err
	}

	err = applyHostTemplate(host, m, apiClient)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to apply host template")
	}

	return m, nil
}

func applyHostTemplate(host *client.Host, m *client.Machine, apiClient *client.RancherClient) error {
	if host.HostTemplateId != "" {
		ht, err := apiClient.HostTemplate.ById(host.HostTemplateId)
		if err != nil {
			return err
		}
		return apply(m, ht, apiClient, true)
	}

	templates, err := apiClient.HostTemplate.List(&client.ListOpts{
		Filters: map[string]interface{}{
			"accountId":    m.AccountId,
			"driver":       m.Driver,
			"removed_null": "true",
			"state":        "active",
		},
	})
	if err != nil {
		return err
	}
	// If we find more than one we apply all secret values, but not public
	if len(templates.Data) > 0 {
		for _, ht := range templates.Data {
			if err := apply(m, &ht, apiClient, false); err != nil {
				return err
			}
		}
	} else if len(templates.Data) == 1 {
		return apply(m, &templates.Data[0], apiClient, true)
	}

	return nil
}

func apply(m *client.Machine, ht *client.HostTemplate, apiClient *client.RancherClient, public bool) error {
	if public {
		if err := copyData(m, ht.PublicValues); err != nil {
			return err
		}
	}

	secretValues := map[string]interface{}{}
	if err := apiClient.GetLink(ht.Resource, "secretValues", &secretValues); err != nil {
		return errors.Wrap(err, "Get secretValues link")
	}

	err := copyData(m, secretValues)
	if err != nil {
		return err
	}

	if err := populateFields(m); err != nil {
		return err
	}

	return err
}

func populateFields(m *client.Machine) error {
	content, err := json.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "populateFields marshall")
	}
	mm := map[string]interface{}{}
	if err := json.Unmarshal(content, &mm); err != nil {
		return errors.Wrap(err, "populateFields unmarshall to mm")
	}
	machineConfig := mm[m.Driver+"Config"]
	if machineConfig == nil {
		return nil
	}
	machineConfigContent, err := json.Marshal(machineConfig)
	if err != nil {
		return errors.Wrap(err, "populateFields marshall machineConfig")
	}
	if m.Data == nil {
		m.Data = map[string]interface{}{}
	}
	fields, ok := m.Data["fields"].(map[string]interface{})
	if !ok {
		fields = map[string]interface{}{}
	}
	driverConfig, ok := fields[m.Driver+"Config"].(map[string]interface{})
	if !ok {
		driverConfig = map[string]interface{}{}
	}
	if err := json.Unmarshal(machineConfigContent, &driverConfig); err != nil {
		return errors.Wrap(err, "populateFields unmarshall to fields")
	}
	for _, key := range []string{"id", "type", "links", "actions"} {
		delete(driverConfig, key)
	}
	fields[m.Driver+"Config"] = driverConfig
	m.Data["fields"] = fields
	return nil
}

func copyData(m *client.Machine, from interface{}) error {
	content, err := json.Marshal(from)
	if err != nil {
		return errors.Wrap(err, "copyData marshall")
	}
	err = json.Unmarshal(content, m)
	if err != nil {
		return errors.Wrap(err, "copyData unmarshall")
	}
	fields := m.Data["fields"]
	err = json.Unmarshal(content, &fields)
	if err != nil {
		return errors.Wrap(err, "copyData unmarshall to fields")
	}
	m.Data["fields"] = fields
	return nil
}

func getHost(m *client.Machine, apiClient *client.RancherClient) (*client.Host, error) {
	hosts, err := apiClient.Host.List(&client.ListOpts{
		Filters: map[string]interface{}{
			"physicalHostId": m.Id,
		},
	})
	if err != nil {
		return nil, err
	}

	if len(hosts.Data) == 0 {
		return nil, err
	}

	return &hosts.Data[0], nil
}

func notAMachineReply(event *events.Event, apiClient *client.RancherClient) error {
	// Called when machine.ById() returned a 404, which indicates this is a
	// physicalHost but not a machine. Just reply.
	reply := newReply(event)
	return publishReply(reply, apiClient)
}

func newReply(event *events.Event) *client.Publish {
	return &client.Publish{
		Name:        event.ReplyTo,
		PreviousIds: []string{event.ID},
	}
}

func cleanupResources(machineDir, name string) error {
	logger.WithFields(logrus.Fields{
		"machine name": name,
	}).Info("starting cleanup...")
	dExists, err := dirExists(machineDir)
	if !dExists {
		return nil
	}

	mExists, err := machineExists(machineDir, name)
	if err != nil {
		return err
	}

	if !mExists {
		return nil
	}

	command, err := buildCommand(machineDir, []string{"rm", "-f", name})
	if err != nil {
		return err
	}

	err = command.Start()
	if err != nil {
		return err
	}

	err = command.Wait()
	if err != nil {
		return err
	}

	removeMachineDir(machineDir)

	logger.WithFields(logrus.Fields{
		"machine name": name,
	}).Info("cleanup successful")
	return nil
}

func dirExists(machineDir string) (bool, error) {
	if _, err := os.Stat(machineDir); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

func preEvent(event *events.Event, apiClient *client.RancherClient) (*client.Machine, *machineInfo, error) {
	machine, err := getMachine(event.ResourceID, apiClient)
	if err != nil {
		return nil, nil, err
	}
	if machine == nil {
		return nil, nil, notAMachineReply(event, apiClient)
	}
	machineName, err := machineCommandName(machine)
	if err != nil {
		return nil, nil, err
	}
	machine.Name = machineName

	machineDir, err := buildBaseMachineDir(machine)
	if err != nil {
		return nil, nil, err
	}

	mInfo := &machineInfo{
		fullMachinePath: machineDir,
		jailDir:         machineDir,
	}

	if os.Getenv("DISABLE_DRIVER_JAIL") != "true" {
		err = createJail(machineDir)
		if err != nil {
			return nil, nil, err
		}
		mInfo.fullMachinePath = path.Join(machineDir, machineDir)
		if err := os.MkdirAll(mInfo.fullMachinePath, 0740); err != nil {
			return nil, nil, err
		}
	}

	err = restoreMachineDir(machine, mInfo.fullMachinePath)
	if err != nil {
		return nil, nil, err
	}

	return machine, mInfo, nil
}

// createJail sets up the named directory for use with chroot
func createJail(machineDir string) error {
	lock.Lock()
	defer lock.Unlock()

	// Check for the done file, if that exists the jail is ready to be used
	_, err := os.Stat(path.Join(machineDir, "done"))
	if err == nil {
		return nil
	}

	// If the base dir exists without the done file rebuild the directory
	_, err = os.Stat(machineDir)
	if err == nil {
		if err := os.RemoveAll(machineDir); err != nil {
			return err
		}
	}

	logrus.Debugf("Creating jail for %v", machineDir)
	// This creates a nested dir, the first nest is the jail root, the 2nd makes everything
	// appear normal for commands being called in the jail - Something like:
	// "/var/lib/cattle/machine/machines/{ExternalId}/var/lib/cattle/machine/machines/{ExternalId}"
	err = os.MkdirAll(path.Join(machineDir, machineDir), 0740)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/jailer.sh", machineDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.WithMessage(err, fmt.Sprintf("error running the jail command: %v", string(out)))
	}
	logrus.Debugf("Output from create jail command %v", string(out))
	return nil
}
