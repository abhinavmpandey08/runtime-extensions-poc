package handler

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
	runtimehooksv1 "sigs.k8s.io/cluster-api/exp/runtime/hooks/api/v1alpha1"
)

type ExtensionHandlers struct {
	client            client.Client
	lastRetryExponent int
}

// NewExtensionHandlers returns a ExtensionHandlers for the lifecycle hooks handlers.
func NewExtensionHandlers(client client.Client) *ExtensionHandlers {
	return &ExtensionHandlers{
		client:            client,
		lastRetryExponent: 0,
	}
}

func (m *ExtensionHandlers) DoBeforeControlPlaneUpdate(ctx context.Context, request *runtimehooksv1.BeforeControlPlaneUpgradeRequest, response *runtimehooksv1.BeforeControlPlaneUpgradeResponse) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("BeforeControlPlaneUpdate is called")

	secret := &corev1.Secret{}
	if err := m.client.Get(ctx, client.ObjectKey{Namespace: request.Cluster.Namespace, Name: "test123"}, secret); err != nil {
		response.Status = runtimehooksv1.ResponseStatusFailure
		response.Message = err.Error()
		response.RetryAfterSeconds = int32(intPow(2, m.lastRetryExponent))
		m.lastRetryExponent++
		log.Info("secret does not exist yet", "err", err.Error(), "retryAfter", response.RetryAfterSeconds)
		log.Info("Following machines need to be upgraded", "MachinesToBeUpgraded", request.MachinesToBeUpgraded)
		return
	}

	// if err := m.readResponseFromConfigMap(ctx, &request.Cluster, runtimehooksv1.BeforeClusterCreate, request.GetSettings(), response); err != nil {
	// 	response.Status = runtimehooksv1.ResponseStatusFailure
	// 	response.Message = err.Error()
	// 	response.RetryAfterSeconds = 10
	// 	return
	// }
	// if err := m.recordCallInConfigMap(ctx, &request.Cluster, runtimehooksv1.BeforeClusterCreate, response); err != nil {
	// 	response.Status = runtimehooksv1.ResponseStatusFailure
	// 	response.Message = err.Error()
	// }
}

func intPow(n, m int) int {
	if m == 0 {
		return 1
	}
	result := n
	for i := 2; i <= m; i++ {
		result *= n
	}
	return result
}

func (m *ExtensionHandlers) readResponseFromConfigMap(ctx context.Context, cluster *clusterv1.Cluster, hook runtimecatalog.Hook, settings map[string]string, response runtimehooksv1.ResponseObject) error {
	hookName := runtimecatalog.HookName(hook)
	configMap := &corev1.ConfigMap{}
	configMapName := fmt.Sprintf("%s-test-extension-hookresponses", cluster.Name)
	if err := m.client.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: configMapName}, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			// A ConfigMap of responses does not exist. Create one now.
			// The ConfigMap is created with blocking responses if "defaultAllHandlersToBlocking" is set to "true"
			// in the settings.
			// This allows the test-extension to have non-blocking behavior by default but can be switched to blocking
			// as needed, example: during E2E testing.
			defaultAllHandlersToBlocking := settings["defaultAllHandlersToBlocking"] == "true"
			configMap = responsesConfigMap(cluster, defaultAllHandlersToBlocking)
			if err := m.client.Create(ctx, configMap); err != nil {
				return errors.Wrapf(err, "failed to create the ConfigMap %s", klog.KRef(cluster.Namespace, configMapName))
			}
		} else {
			return errors.Wrapf(err, "failed to read the ConfigMap %s", klog.KRef(cluster.Namespace, configMapName))
		}
	}
	if err := yaml.Unmarshal([]byte(configMap.Data[hookName+"-preloadedResponse"]), response); err != nil {
		return errors.Wrapf(err, "failed to read %q response information from ConfigMap", hook)
	}
	if r, ok := response.(runtimehooksv1.RetryResponseObject); ok {
		log := ctrl.LoggerFrom(ctx)
		log.Info(fmt.Sprintf("%s response is %s. retry: %v", hookName, r.GetStatus(), r.GetRetryAfterSeconds()))
	}
	return nil
}

// responsesConfigMap generates a ConfigMap with preloaded responses for the test extension.
// If defaultAllHandlersToBlocking is set to true, all the preloaded responses are set to blocking.
func responsesConfigMap(cluster *clusterv1.Cluster, defaultAllHandlersToBlocking bool) *corev1.ConfigMap {
	retryAfterSeconds := 0
	if defaultAllHandlersToBlocking {
		retryAfterSeconds = 5
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-test-extension-hookresponses", cluster.Name),
			Namespace: cluster.Namespace,
		},
		// Set the initial preloadedResponses for each of the tested hooks.
		Data: map[string]string{
			// Blocking hooks are set to return RetryAfterSeconds initially. These will be changed during the test.
			"BeforeClusterCreate-preloadedResponse":      fmt.Sprintf(`{"Status": "Success", "RetryAfterSeconds": %d}`, retryAfterSeconds),
			"BeforeClusterUpgrade-preloadedResponse":     fmt.Sprintf(`{"Status": "Success", "RetryAfterSeconds": %d}`, retryAfterSeconds),
			"AfterControlPlaneUpgrade-preloadedResponse": fmt.Sprintf(`{"Status": "Success", "RetryAfterSeconds": %d}`, retryAfterSeconds),
			"BeforeClusterDelete-preloadedResponse":      fmt.Sprintf(`{"Status": "Success", "RetryAfterSeconds": %d}`, retryAfterSeconds),

			// Non-blocking hooks are set to Status:Success.
			"AfterControlPlaneInitialized-preloadedResponse": `{"Status": "Success"}`,
			"AfterClusterUpgrade-preloadedResponse":          `{"Status": "Success"}`,
		},
	}
}

func (m *ExtensionHandlers) recordCallInConfigMap(ctx context.Context, cluster *clusterv1.Cluster, hook runtimecatalog.Hook, response runtimehooksv1.ResponseObject) error {
	hookName := runtimecatalog.HookName(hook)
	configMap := &corev1.ConfigMap{}
	configMapName := fmt.Sprintf("%s-test-extension-hookresponses", cluster.Name)
	if err := m.client.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: configMapName}, configMap); err != nil {
		return errors.Wrapf(err, "failed to read the ConfigMap %s", klog.KRef(cluster.Namespace, configMapName))
	}
	var patch client.Patch
	if r, ok := response.(runtimehooksv1.RetryResponseObject); ok {
		patch = client.RawPatch(types.MergePatchType,
			[]byte(fmt.Sprintf(`{"data":{"%s-actualResponseStatus": "Status: %s, RetryAfterSeconds: %v"}}`, hookName, r.GetStatus(), r.GetRetryAfterSeconds())))
	} else {
		// Patch the actualResponseStatus with the returned value
		patch = client.RawPatch(types.MergePatchType,
			[]byte(fmt.Sprintf(`{"data":{"%s-actualResponseStatus":"%s"}}`, hookName, response.GetStatus()))) //nolint:gocritic
	}
	if err := m.client.Patch(ctx, configMap, patch); err != nil {
		return errors.Wrapf(err, "failed to update the ConfigMap %s", klog.KRef(cluster.Namespace, configMapName))
	}
	return nil
}
