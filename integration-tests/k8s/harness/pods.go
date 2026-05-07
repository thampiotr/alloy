package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	timeout       = 5 * time.Minute
	retryInterval = 500 * time.Millisecond
)

// WaitForAllPodsRunning is the test-facing wait: it fails the test on
// timeout. Use AwaitAllPodsRunning from non-test code (e.g. dependency
// Install hooks) where you want a returned error instead.
func (ctx *TestContext) WaitForAllPodsRunning(t *testing.T, namespace, labelSelector string) {
	t.Helper()
	require.NoError(t, ctx.AwaitAllPodsRunning(namespace, labelSelector))
}

// AwaitAllPodsRunning polls the API server until at least one pod matches
// labelSelector in namespace and they are all in PodRunning phase, or the
// shared timeout expires. It returns the latest reason for failure on
// timeout so callers can wrap it with their own context.
func (ctx *TestContext) AwaitAllPodsRunning(namespace, labelSelector string) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		pods, err := ctx.client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		switch {
		case err != nil:
			lastErr = err
		case len(pods.Items) == 0:
			lastErr = fmt.Errorf("no pods for namespace=%s selector=%s", namespace, labelSelector)
		default:
			lastErr = nil
			for _, pod := range pods.Items {
				if pod.DeletionTimestamp != nil {
					lastErr = fmt.Errorf("pod %s is deleting", pod.Name)
					break
				}
				if pod.Status.Phase != corev1.PodRunning {
					lastErr = fmt.Errorf("pod %s is %s", pod.Name, pod.Status.Phase)
					break
				}
			}
			if lastErr == nil {
				return nil
			}
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("timed out waiting for pods (namespace=%s selector=%s): %w", namespace, labelSelector, lastErr)
		}
		time.Sleep(retryInterval)
	}
}
