package ops

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ListNamespaces returns all namespace names for the named context.
func (o *Ops) ListNamespaces(ctxName string, _ time.Duration) ([]string, error) {
	cs, err := o.clientset(ctxName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces for context %q: %w", ctxName, err)
	}
	names := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		names = append(names, ns.Name)
	}
	sort.Strings(names)
	return names, nil
}

// SwitchNamespace updates the namespace field of ctxName in its kubeconfig.
func (o *Ops) SwitchNamespace(ctxName, ns string) error {
	nc, _, err := o.Mgr.FindContext(ctxName)
	if err != nil {
		return err
	}
	if _, err := core_Backup(nc.Path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	nc.Lock()
	nc.Config.Contexts[ctxName].Namespace = ns
	cfg := nc.Config
	nc.Unlock()
	if err := core_WriteFile(nc.Path, cfg); err != nil {
		return err
	}
	return o.Mgr.Load(nc.Path)
}

// PodCounts returns namespace → pod count for all namespaces in ctxName.
func (o *Ops) PodCounts(ctxName string) (map[string]int, error) {
	cs, err := o.clientset(ctxName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	nsList, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(nsList.Items))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ns := range nsList.Items {
		wg.Add(1)
		go func(nsName string) {
			defer wg.Done()
			pods, err := cs.CoreV1().Pods(nsName).List(ctx, metav1.ListOptions{})
			if err != nil {
				return
			}
			mu.Lock()
			counts[nsName] = len(pods.Items)
			mu.Unlock()
		}(ns.Name)
	}
	wg.Wait()
	return counts, nil
}

// CheckHealth pings the API server of ctxName and returns a HealthStatus.
func (o *Ops) CheckHealth(ctxName string, timeout time.Duration) HealthStatus {
	hs := HealthStatus{ContextName: ctxName}

	nc, _, err := o.Mgr.FindContext(ctxName)
	if err != nil {
		hs.Err = err.Error()
		return hs
	}
	nc.RLock()
	ctx2 := nc.Config.Contexts[ctxName]
	if ctx2 != nil {
		if cl, ok := nc.Config.Clusters[ctx2.Cluster]; ok {
			hs.Server = cl.Server
		}
	}
	nc.RUnlock()

	cs, err := o.clientset(ctxName)
	if err != nil {
		hs.Err = err.Error()
		return hs
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err = cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	hs.Latency = time.Since(start)
	if err != nil {
		hs.Err = err.Error()
	} else {
		hs.OK = true
	}
	return hs
}

// BulkHealth checks health for all contexts in parallel.
func (o *Ops) BulkHealth(timeout time.Duration) []HealthStatus {
	contexts := o.Mgr.AllContexts()
	results := make([]HealthStatus, len(contexts))
	var wg sync.WaitGroup
	for i, ci := range contexts {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = o.CheckHealth(name, timeout)
		}(i, ci.Name)
	}
	wg.Wait()
	return results
}

// clientset builds a typed Kubernetes client for the named context.
func (o *Ops) clientset(ctxName string) (*kubernetes.Clientset, error) {
	nc, _, err := o.Mgr.FindContext(ctxName)
	if err != nil {
		return nil, err
	}
	nc.RLock()
	// Write config to bytes using client-go.
	cfgCopy := nc.Config.DeepCopy()
	nc.RUnlock()

	raw, err := clientcmd.Write(*cfgCopy)
	if err != nil {
		return nil, fmt.Errorf("serialising config for %q: %w", ctxName, err)
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("building REST config for %q: %w", ctxName, err)
	}
	restCfg.Timeout = 10 * time.Second
	return kubernetes.NewForConfig(restCfg)
}

// DeepCopy shim so we can copy without the full apimachinery dep in tests.
func init() { _ = clientcmdapi.Config{} }

// Aliases back to core package — defined in merge.go to keep one import.
var core_Backup   = Backup
var core_WriteFile = WriteFile
