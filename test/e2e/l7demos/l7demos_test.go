// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package l7demos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/cilium/cilium/pkg/e2ecluster/ciliuminstall"
	"github.com/cilium/cilium/pkg/e2ecluster/e2ehelpers"
	"github.com/cilium/cilium/test/helpers"
)

var (
	testenv env.Environment
	pwd     string

	namespace string = envconf.RandomName("l7demos-ns", 16)
)

func TestMain(m *testing.M) {
	cfg, err := envconf.NewFromFlags()
	if err != nil {
		klog.Fatalf("failed to build envconf from flags: %s", err)
	}

	pwd, err = os.Getwd()
	if err != nil {
		klog.Fatal(err)
	}

	// TODO: how to avoid having to set this in each TestMain?
	chartDirectory := filepath.Join(pwd, "..", "..", "..", "install", "kubernetes", "cilium")

	testenv = env.NewWithConfig(cfg)
	ciliumHelmOpts := map[string]string{
		// empty opts
	}
	testenv.Setup(
		e2ehelpers.MaybeCreateTempKindCluster(testenv, "l7demos"),
		envfuncs.CreateNamespace(namespace),
		ciliuminstall.Setup(
			ciliuminstall.WithChartDirectory(chartDirectory),
			ciliuminstall.WithHelmOptions(ciliumHelmOpts),
		),
	)
	testenv.Finish(
		ciliuminstall.Finish(
			ciliuminstall.WithChartDirectory(chartDirectory),
		),
		//		envfuncs.DeleteNamespace(namespace),
	)
	os.Exit(testenv.Run(m))
}

func TestStarWarsDemo(t *testing.T) {
	// TODO: replace by Go struct definitions?
	manifests := os.DirFS(filepath.Join(pwd, "..", "..", "k8s", "manifests", "star-wars-demo"))
	pattern := "*.yaml"

	starWarsDemoFeature := features.New("l7 demos").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// load manifests
			r, err := resources.New(cfg.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}
			if err := decoder.DecodeEachFile(
				ctx,
				manifests,
				pattern,
				decoder.CreateHandler(r),           // try to CREATE objects after decoding
				decoder.MutateNamespace(namespace), // inject a namespace into decoded objects, before calling CreateHandler
			); err != nil {
				t.Fatal(err)
			}
			return ctx
		}).
		Assess("Star Wars Demo", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			// check for decoded object object creation
			if err := client.Resources(namespace).Get(ctx, "deathstar", namespace, &corev1.Service{}); err != nil {
				t.Fatal(err)
			}
			if err := client.Resources(namespace).Get(ctx, "deathstar", namespace, &appsv1.Deployment{}); err != nil {
				t.Fatal(err)
			}
			if err := client.Resources(namespace).Get(ctx, "spaceship", namespace, &appsv1.Deployment{}); err != nil {
				t.Fatal(err)
			}
			var xwingDeployment appsv1.Deployment
			if err := client.Resources(namespace).Get(ctx, "xwing", namespace, &xwingDeployment); err != nil {
				t.Fatal(err)
			}

			// TODO(tklauser): wait for deployment

			var xwingPods corev1.PodList
			if err := client.Resources(namespace).List(
				ctx,
				&xwingPods,
				resources.WithLabelSelector(
					labels.FormatLabels(
						map[string]string{
							"org":   "alliance",
							"class": "spaceship",
						},
					),
				)); err != nil {
				t.Fatal(err)
			}
			if len(xwingPods.Items) == 0 {
				t.Fatalf("no xwing pods found")
			}
			t.Logf("found %d pods", len(xwingPods.Items))
			xwingPod := xwingPods.Items[0]
			t.Logf("using xwing pod %v", xwingPod)

			deathstarServiceName := "deathstar"
			deathstarFQDN := fmt.Sprintf("%s.%s.svc.cluster.local", deathstarServiceName, helpers.DefaultNamespace)

			cmd := e2ehelpers.Curl(
				fmt.Sprintf("curl http://%s/v1", deathstarFQDN),
				e2ehelpers.WithFail(true),
				e2ehelpers.WithResultFormat(e2ehelpers.CurlResultFormatStats),
			)
			namespace := xwingPod.Namespace
			name := xwingPod.Name
			container := xwingPod.Spec.Containers[0].Name
			t.Logf("Executing cURL command %s in pod %s/%s (container %s)", strings.Join(cmd, " "), namespace, name, container)
			out, err := e2ehelpers.ExecInPodCombinedOutput(ctx, client, namespace, name, container, cmd)
			t.Logf("Execing curl returned output: %s", out)
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return ctx
		}).Feature()
	testenv.Test(t, starWarsDemoFeature)
}
