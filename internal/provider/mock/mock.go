package mock

import (
	"context"
	"fmt"
	"github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/trace"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1"
)

const (
	// Provider configuration defaults.
	defaultCPUCapacity    = "20"
	defaultMemoryCapacity = "100Gi"
	defaultPodCapacity    = "20"

	// Values used in tracing as attribute keys.
	namespaceKey     = "namespace"
	nameKey          = "name"
	containerNameKey = "containerName"
)

// See: https://github.com/virtual-kubelet/virtual-kubelet/issues/632
/*
var (
	_ providers.Provider           = (*MockV0Provider)(nil)
	_ providers.PodMetricsProvider = (*MockV0Provider)(nil)
	_ node.PodNotifier         = (*MockProvider)(nil)
)
*/

// MockProvider implements the mocklet provider interface and stores pods in memory.
type MockProvider struct { // nolint:golint
	nodeName           string
	operatingSystem    string
	internalIP         string
	daemonEndpointPort int32
	pods               map[string]*v1.Pod
	config             MockConfig
	startTime          time.Time
	notifier           func(*v1.Pod)
}

// MockConfig contains a mock mocklet's configurable parameters.
type MockConfig struct { //nolint:golint
	CPU    string `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	Pods   string `yaml:"pods,omitempty"`
}

// NewMockProviderMockConfig creates a new MockV0Provider. Mock legacy provider does not implement the new asynchronous podnotifier interface
func NewMockProviderMockConfig(config MockConfig, nodeName, operatingSystem string, internalIP string, daemonEndpointPort int32) (*MockProvider, error) {
	//set defaults
	if config.CPU == "" {
		config.CPU = defaultCPUCapacity
	}
	if config.Memory == "" {
		config.Memory = defaultMemoryCapacity
	}
	if config.Pods == "" {
		config.Pods = defaultPodCapacity
	}
	provider := MockProvider{
		nodeName:           nodeName,
		operatingSystem:    operatingSystem,
		internalIP:         internalIP,
		daemonEndpointPort: daemonEndpointPort,
		pods:               make(map[string]*v1.Pod),
		config:             config,
		startTime:          time.Now(),
	}

	return &provider, nil
}

// NewMockProvider creates a new MockProvider, which implements the PodNotifier interface
func NewMockProvider(providerConfig, nodeName, operatingSystem string, internalIP string, daemonEndpointPort int32) (*MockProvider, error) {
	config, err := loadConfig(providerConfig, nodeName)
	if err != nil {
		return nil, err
	}

	return NewMockProviderMockConfig(config, nodeName, operatingSystem, internalIP, daemonEndpointPort)
}

// loadConfig loads the given json configuration files.
func loadConfig(providerConfig, nodeName string) (config MockConfig, err error) {
	fmt.Println(providerConfig)
	if providerConfig != "" {
		data, err := ioutil.ReadFile(providerConfig)
		if err != nil {
			return config, err
		}
		configMap := map[string]MockConfig{}
		fmt.Printf("Mock Config : %s", string(data))
		fmt.Printf("NodeName : %s", nodeName)
		err = yaml.Unmarshal(data, configMap)
		if err != nil {
			return config, err
		}

		configMap["mockNode"] = MockConfig{
			CPU:    "1000",
			Memory: "213",
			Pods:   "300",
		}

		//yamlData, _ := yaml.Marshal(&configMap)
		//fmt.Printf("yamldata : %s", string(yamlData))

		//fmt.Printf("\nMarshalled successfully, CPU = %s", j, _ := json.Marshal(configMap); string(j))
		if _, exist := configMap[nodeName]; exist {
			fmt.Printf("\nNode : %s, Exists", nodeName)
			config = configMap[nodeName]
			if config.CPU == "" {
				config.CPU = defaultCPUCapacity
			}
			if config.Memory == "" {
				config.Memory = defaultMemoryCapacity
			}
			if config.Pods == "" {
				config.Pods = defaultPodCapacity
			}
		} else {
			fmt.Printf("\nNot exists node : %s", nodeName)
		}
	} else {
		fmt.Println("\nGetting from ENV variables")
		config.Pods = os.Getenv("NUMBER_OF_PODS")
		config.CPU = os.Getenv("NODE_CPU")
		config.Memory = os.Getenv("NODE_MEMORY")
	}

	fmt.Printf("\nUsing config as number of pods= %s, node cpu = %s, node memory = %s", config.Pods, config.CPU, config.Memory)

	if _, err = resource.ParseQuantity(config.CPU); err != nil {
		return config, fmt.Errorf("invalid CPU value %s, %v", config.CPU, err)
	}
	if _, err = resource.ParseQuantity(config.Memory); err != nil {
		return config, fmt.Errorf("Invalid memory value %v", config.Memory)
	}
	if _, err = resource.ParseQuantity(config.Pods); err != nil {
		return config, fmt.Errorf("Invalid pods value %v", config.Pods)
	}
	return config, nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

var sub3 = os.Getenv("POD_IP_SUB_START")
var podCounter = 1

// CreatePod accepts a Pod definition and stores it in memory.
func (p *MockProvider) CreatePod(ctx context.Context, pod *v1.Pod) error {
	ctx, span := trace.StartSpan(ctx, "CreatePod")
	defer span.End()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, pod.Namespace, nameKey, pod.Name)

	log.G(ctx).Infof("receive CreatePod %q", pod.Name)

	key, err := buildKey(pod)
	if err != nil {
		return err
	}
	now := metav1.NewTime(time.Now())
	//random := rand.Int() % 255

	if podCounter > 255 {
		sub3Int, errConv := strconv.Atoi(sub3)
		if errConv != nil {
			fmt.Errorf(errConv.Error())
		}
		sub3Int++
		sub3 = fmt.Sprintf("%d", sub3Int)
		podCounter = 1
	}
	podIp := fmt.Sprintf("%s.%s.%d", os.Getenv("POD_IP_PREFIX"), sub3, podCounter)
	podCounter++
	pod.Status = v1.PodStatus{
		Phase:     v1.PodRunning,
		HostIP:    os.Getenv("VKUBELET_POD_IP"),
		PodIP:     podIp,
		StartTime: &now,
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodInitialized,
				Status: v1.ConditionTrue,
			},
			{
				Type:   v1.PodReady,
				Status: v1.ConditionTrue,
			},
			{
				Type:   v1.PodScheduled,
				Status: v1.ConditionTrue,
			},
		},
	}

	pod.SelfLink = fmt.Sprintf("/api/v1/namespace/%s/pod/%s", pod.Namespace, pod.Name)

	for _, container := range pod.Spec.Containers {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
			Name:         container.Name,
			Image:        container.Image,
			Ready:        true,
			RestartCount: 0,
			State: v1.ContainerState{
				Running: &v1.ContainerStateRunning{
					StartedAt: now,
				},
			},
			ContainerID: RandStringRunes(64),
		})
	}

	p.pods[key] = pod
	p.notifier(pod)

	return nil
}

// UpdatePod accepts a Pod definition and updates its reference.
func (p *MockProvider) UpdatePod(ctx context.Context, pod *v1.Pod) error {
	ctx, span := trace.StartSpan(ctx, "UpdatePod")
	defer span.End()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, pod.Namespace, nameKey, pod.Name)

	log.G(ctx).Infof("receive UpdatePod %q", pod.Name)

	key, err := buildKey(pod)
	if err != nil {
		return err
	}

	p.pods[key] = pod
	p.notifier(pod)

	return nil
}

// DeletePod deletes the specified pod out of memory.
func (p *MockProvider) DeletePod(ctx context.Context, pod *v1.Pod) (err error) {
	ctx, span := trace.StartSpan(ctx, "DeletePod")
	defer span.End()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, pod.Namespace, nameKey, pod.Name)

	log.G(ctx).Infof("receive DeletePod %q", pod.Name)

	key, err := buildKey(pod)
	if err != nil {
		return err
	}

	if _, exists := p.pods[key]; !exists {
		return errdefs.NotFound("pod not found")
	}

	now := metav1.Now()
	delete(p.pods, key)
	pod.Status.Phase = v1.PodSucceeded
	pod.Status.Reason = "MockProviderPodDeleted"

	for idx := range pod.Status.ContainerStatuses {
		pod.Status.ContainerStatuses[idx].Ready = false
		pod.Status.ContainerStatuses[idx].State = v1.ContainerState{
			Terminated: &v1.ContainerStateTerminated{
				Message:    "Mock provider terminated container upon deletion",
				FinishedAt: now,
				Reason:     "MockProviderPodContainerDeleted",
				StartedAt:  pod.Status.ContainerStatuses[idx].State.Running.StartedAt,
			},
		}
	}

	p.notifier(pod)

	return nil
}

// GetPod returns a pod by name that is stored in memory.
func (p *MockProvider) GetPod(ctx context.Context, namespace, name string) (pod *v1.Pod, err error) {
	ctx, span := trace.StartSpan(ctx, "GetPod")
	defer func() {
		span.SetStatus(err)
		span.End()
	}()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, namespace, nameKey, name)

	log.G(ctx).Infof("receive GetPod %q", name)

	key, err := buildKeyFromNames(namespace, name)
	if err != nil {
		return nil, err
	}

	if pod, ok := p.pods[key]; ok {
		return pod, nil
	}
	return nil, errdefs.NotFoundf("pod \"%s/%s\" is not known to the provider", namespace, name)
}

// GetContainerLogs retrieves the logs of a container by name from the provider.
func (p *MockProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	ctx, span := trace.StartSpan(ctx, "GetContainerLogs")
	defer span.End()

	// Add pod and container attributes to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, namespace, nameKey, podName, containerNameKey, containerName)

	log.G(ctx).Infof("receive GetContainerLogs %q", podName)
	return ioutil.NopCloser(strings.NewReader("")), nil
}

// RunInContainer executes a command in a container in the pod, copying data
// between in/out/err and the container's stdin/stdout/stderr.
func (p *MockProvider) RunInContainer(ctx context.Context, namespace, name, container string, cmd []string, attach api.AttachIO) error {
	log.G(context.TODO()).Infof("receive ExecInContainer %q", container)
	return nil
}

// GetPodStatus returns the status of a pod by name that is "running".
// returns nil if a pod by that name is not found.
func (p *MockProvider) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	ctx, span := trace.StartSpan(ctx, "GetPodStatus")
	defer span.End()

	// Add namespace and name as attributes to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, namespace, nameKey, name)

	log.G(ctx).Infof("receive GetPodStatus %q", name)

	pod, err := p.GetPod(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	return &pod.Status, nil
}

// GetPods returns a list of all pods known to be "running".
func (p *MockProvider) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	ctx, span := trace.StartSpan(ctx, "GetPods")
	defer span.End()

	log.G(ctx).Info("receive GetPods")

	var pods []*v1.Pod

	for _, pod := range p.pods {
		pods = append(pods, pod)
	}

	return pods, nil
}

func (p *MockProvider) ConfigureNode(ctx context.Context, n *v1.Node) {
	ctx, span := trace.StartSpan(ctx, "mock.ConfigureNode") //nolint:ineffassign
	defer span.End()

	n.Status.Capacity = p.capacity()
	n.Status.Allocatable = p.capacity()
	n.Status.Conditions = p.nodeConditions()
	n.Status.Addresses = p.nodeAddresses()
	n.Status.DaemonEndpoints = p.nodeDaemonEndpoints()
	os := p.operatingSystem
	if os == "" {
		os = "Linux"
	}
	n.Status.NodeInfo.OperatingSystem = os
	n.Status.NodeInfo.Architecture = "amd64"
	n.ObjectMeta.Labels["alpha.service-controller.kubernetes.io/exclude-balancer"] = "true"
}

// Capacity returns a resource list containing the capacity limits.
func (p *MockProvider) capacity() v1.ResourceList {
	return v1.ResourceList{
		"cpu":    resource.MustParse(p.config.CPU),
		"memory": resource.MustParse(p.config.Memory),
		"pods":   resource.MustParse(p.config.Pods),
	}
}

// NodeConditions returns a list of conditions (Ready, OutOfDisk, etc), for updates to the node status
// within Kubernetes.
func (p *MockProvider) nodeConditions() []v1.NodeCondition {
	// TODO: Make this configurable
	return []v1.NodeCondition{
		{
			Type:               "Ready",
			Status:             v1.ConditionTrue,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletReady",
			Message:            "kubelet is ready.",
		},
		{
			Type:               "OutOfDisk",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientDisk",
			Message:            "kubelet has sufficient disk space available",
		},
		{
			Type:               "MemoryPressure",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientMemory",
			Message:            "kubelet has sufficient memory available",
		},
		{
			Type:               "DiskPressure",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasNoDiskPressure",
			Message:            "kubelet has no disk pressure",
		},
		{
			Type:               "NetworkUnavailable",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "RouteCreated",
			Message:            "RouteController created a route",
		},
	}

}

// NodeAddresses returns a list of addresses for the node status
// within Kubernetes.
func (p *MockProvider) nodeAddresses() []v1.NodeAddress {
	return []v1.NodeAddress{
		{
			Type:    "InternalIP",
			Address: p.internalIP,
		},
		{
			Type:    "Hostname",
			Address: p.nodeName,
		},
	}
}

// NodeDaemonEndpoints returns NodeDaemonEndpoints for the node status
// within Kubernetes.
func (p *MockProvider) nodeDaemonEndpoints() v1.NodeDaemonEndpoints {
	return v1.NodeDaemonEndpoints{
		KubeletEndpoint: v1.DaemonEndpoint{
			Port: p.daemonEndpointPort,
		},
	}
}

// GetStatsSummary returns dummy stats for all pods known by this provider.
func (p *MockProvider) GetStatsSummary(ctx context.Context) (*statsv1alpha1.Summary, error) {
	// Create the Summary object that will later be populated with node and pod stats.
	res := &statsv1alpha1.Summary{}

	// Grab the current timestamp so we can report it as the time the stats were generated.
	time := metav1.NewTime(time.Now())

	// Populate the Summary object with basic node stats.
	res.Node = statsv1alpha1.NodeStats{
		NodeName:  p.nodeName,
		StartTime: metav1.NewTime(p.startTime),
	}

	// Populate the Summary object with dummy stats for each pod known by this provider.
	for _, pod := range p.pods {
		var (
			// totalUsageNanoCores will be populated with the sum of the values of UsageNanoCores computes across all containers in the pod.
			totalUsageNanoCores uint64
			// totalUsageBytes will be populated with the sum of the values of UsageBytes computed across all containers in the pod.
			totalUsageBytes uint64
		)

		// Create a PodStats object to populate with pod stats.
		pss := statsv1alpha1.PodStats{
			PodRef: statsv1alpha1.PodReference{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				UID:       string(pod.UID),
			},
			StartTime: pod.CreationTimestamp,
		}

		// Iterate over all containers in the current pod to compute dummy stats.
		for _, container := range pod.Spec.Containers {
			// Grab a dummy value to be used as the total CPU usage.
			// The value should fit a uint32 in order to avoid overflows later on when computing pod stats.
			dummyUsageNanoCores := uint64(rand.Uint32())
			totalUsageNanoCores += dummyUsageNanoCores
			// Create a dummy value to be used as the total RAM usage.
			// The value should fit a uint32 in order to avoid overflows later on when computing pod stats.
			dummyUsageBytes := uint64(rand.Uint32())
			totalUsageBytes += dummyUsageBytes
			// Append a ContainerStats object containing the dummy stats to the PodStats object.
			pss.Containers = append(pss.Containers, statsv1alpha1.ContainerStats{
				Name:      container.Name,
				StartTime: pod.CreationTimestamp,
				CPU: &statsv1alpha1.CPUStats{
					Time:           time,
					UsageNanoCores: &dummyUsageNanoCores,
				},
				Memory: &statsv1alpha1.MemoryStats{
					Time:       time,
					UsageBytes: &dummyUsageBytes,
				},
			})

			// Populate the CPU and RAM stats for the pod and append the PodsStats object to the Summary object to be returned.
			pss.CPU = &statsv1alpha1.CPUStats{
				Time:           time,
				UsageNanoCores: &totalUsageNanoCores,
			}
			pss.Memory = &statsv1alpha1.MemoryStats{
				Time:       time,
				UsageBytes: &totalUsageBytes,
			}
			res.Pods = append(res.Pods, pss)
		}
	}

	// Return the dummy stats.
	return res, nil
}

/*func (p *MockProvider) GetStatsSummary(ctx context.Context) (*stats.Summary, error) {
var span trace.Span
ctx, span = trace.StartSpan(ctx, "GetStatsSummary") //nolint: ineffassign
defer span.End()

// Grab the current timestamp so we can report it as the time the stats were generated.
time := metav1.NewTime(time.Now())

// Create the Summary object that will later be populated with node and pod stats.
//res := &stats.Summary{}

// Populate the Summary object with basic node stats.
/*res.Node = stats.NodeStats{
	NodeName:  p.nodeName,
	StartTime: metav1.NewTime(p.startTime),
}*/

// Populate the Summary object with dummy stats for each pod known by this provider.
/*for _, pod := range p.pods {
var (
	// totalUsageNanoCores will be populated with the sum of the values of UsageNanoCores computes across all containers in the pod.
	totalUsageNanoCores uint64
	// totalUsageBytes will be populated with the sum of the values of UsageBytes computed across all containers in the pod.
	totalUsageBytes uint64
)

// Iterate over all containers in the current pod to compute dummy stats.
for _, container := range pod.Spec.Containers {
	// Grab a dummy value to be used as the total CPU usage.
	// The value should fit a uint32 in order to avoid overflows later on when computing pod stats.
	dummyUsageNanoCores := uint64(rand.Uint32())
	totalUsageNanoCores += dummyUsageNanoCores
	// Create a dummy value to be used as the total RAM usage.
	// The value should fit a uint32 in order to avoid overflows later on when computing pod stats.
	dummyUsageBytes := uint64(rand.Uint32())
	totalUsageBytes += dummyUsageBytes
	// Append a ContainerStats object containing the dummy stats to the PodStats object.
	/*pss.Containers = append(pss.Containers, stats.ContainerStats{
		Name:      container.Name,
		StartTime: pod.CreationTimestamp,
		CPU: &stats.CPUStats{
			Time:           time,
			UsageNanoCores: &dummyUsageNanoCores,
		},
		Memory: &stats.MemoryStats{
			Time:       time,
			UsageBytes: &dummyUsageBytes,
		},
	})*/
/*}

		// Populate the CPU and RAM stats for the pod and append the PodsStats object to the Summary object to be returned.
		pss.CPU = &stats.CPUStats{
			Time:           time,
			UsageNanoCores: &totalUsageNanoCores,
		}
		pss.Memory = &stats.MemoryStats{
			Time:       time,
			UsageBytes: &totalUsageBytes,
		}
		res.Pods = append(res.Pods, pss)
	}

	// Return the dummy stats.
	return res, nil
}*/

// NotifyPods is called to set a pod notifier callback function. This should be called before any operations are done
// within the provider.
func (p *MockProvider) NotifyPods(ctx context.Context, notifier func(*v1.Pod)) {
	p.notifier = notifier
}

func buildKeyFromNames(namespace string, name string) (string, error) {
	return fmt.Sprintf("%s-%s", namespace, name), nil
}

// buildKey is a helper for building the "key" for the providers pod store.
func buildKey(pod *v1.Pod) (string, error) {
	if pod.ObjectMeta.Namespace == "" {
		return "", fmt.Errorf("pod namespace not found")
	}

	if pod.ObjectMeta.Name == "" {
		return "", fmt.Errorf("pod name not found")
	}

	return buildKeyFromNames(pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
}

// addAttributes adds the specified attributes to the provided span.
// attrs must be an even-sized list of string arguments.
// Otherwise, the span won't be modified.
// TODO: Refactor and move to a "tracing utilities" package.
func addAttributes(ctx context.Context, span trace.Span, attrs ...string) context.Context {
	if len(attrs)%2 == 1 {
		return ctx
	}
	for i := 0; i < len(attrs); i += 2 {
		ctx = span.WithField(ctx, attrs[i], attrs[i+1])
	}
	return ctx
}
