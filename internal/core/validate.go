package core

import (
	"fmt"
	"strings"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Validate checks a single Config for common issues and returns a list of
// ValidationIssues. It never returns an error itself — problems are reported
// as issues.
func Validate(path string, cfg *clientcmdapi.Config) []ValidationIssue {
	var issues []ValidationIssue

	warn := func(ctx, msg string) {
		issues = append(issues, ValidationIssue{File: path, Context: ctx, Severity: "warning", Message: msg})
	}
	errIssue := func(ctx, msg string) {
		issues = append(issues, ValidationIssue{File: path, Context: ctx, Severity: "error", Message: msg})
	}

	// Check current-context reference.
	if cfg.CurrentContext != "" {
		if _, ok := cfg.Contexts[cfg.CurrentContext]; !ok {
			errIssue("", fmt.Sprintf("current-context %q references non-existent context", cfg.CurrentContext))
		}
	}

	// Check every context.
	for name, ctx := range cfg.Contexts {
		if ctx.Cluster == "" {
			warn(name, "context has no cluster reference")
		} else if _, ok := cfg.Clusters[ctx.Cluster]; !ok {
			errIssue(name, fmt.Sprintf("references cluster %q which is not defined", ctx.Cluster))
		}

		if ctx.AuthInfo == "" {
			warn(name, "context has no user reference")
		} else if _, ok := cfg.AuthInfos[ctx.AuthInfo]; !ok {
			errIssue(name, fmt.Sprintf("references user %q which is not defined", ctx.AuthInfo))
		}
	}

	// Detect orphaned clusters.
	usedClusters := make(map[string]bool)
	for _, ctx := range cfg.Contexts {
		usedClusters[ctx.Cluster] = true
	}
	for name := range cfg.Clusters {
		if !usedClusters[name] {
			warn("", fmt.Sprintf("cluster %q is defined but not referenced by any context", name))
		}
	}

	// Detect orphaned users.
	usedUsers := make(map[string]bool)
	for _, ctx := range cfg.Contexts {
		usedUsers[ctx.AuthInfo] = true
	}
	for name := range cfg.AuthInfos {
		if !usedUsers[name] {
			warn("", fmt.Sprintf("user %q is defined but not referenced by any context", name))
		}
	}

	// Detect clusters with empty server URL.
	for name, cl := range cfg.Clusters {
		if strings.TrimSpace(cl.Server) == "" {
			errIssue("", fmt.Sprintf("cluster %q has an empty server URL", name))
		}
	}

	return issues
}

// DetectConflicts compares two configs and reports contexts that share a name
// but have different cluster/user/namespace settings.
func DetectConflicts(path1 string, cfg1 *clientcmdapi.Config, path2 string, cfg2 *clientcmdapi.Config) []ValidationIssue {
	var issues []ValidationIssue
	for name, ctx1 := range cfg1.Contexts {
		ctx2, ok := cfg2.Contexts[name]
		if !ok {
			continue
		}
		if ctx1.Cluster != ctx2.Cluster || ctx1.AuthInfo != ctx2.AuthInfo || ctx1.Namespace != ctx2.Namespace {
			issues = append(issues, ValidationIssue{
				File:     path1 + " vs " + path2,
				Context:  name,
				Severity: "warning",
				Message: fmt.Sprintf(
					"context %q defined in both files with different settings (cluster: %s/%s, user: %s/%s, namespace: %s/%s)",
					name, ctx1.Cluster, ctx2.Cluster, ctx1.AuthInfo, ctx2.AuthInfo, ctx1.Namespace, ctx2.Namespace,
				),
			})
		}
	}
	return issues
}
