package controller

import (
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

var (
	cfg            *rest.Config
	k8sClient      client.Client
	testEnv        *envtest.Environment
	testEnvStarted bool
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		BinaryAssetsDirectory: envtestAssetsDirectory(),
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "test", "fixtures", "cnpg-crds.yaml"),
			filepath.Join("..", "..", "..", "deploy", "platform", "chart", "files", "rustfs-operator", "tenant-crd.yaml"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	testEnvStarted = true
	Expect(cfg).NotTo(BeNil())

	err = tamossv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = apiextensionsv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = cnpgv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = gatewayv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if !shouldStopTestEnv(testEnv, testEnvStarted) {
		return
	}
	err := testEnv.Stop()
	testEnvStarted = false
	Expect(err).NotTo(HaveOccurred())
})

func shouldStopTestEnv(env *envtest.Environment, started bool) bool {
	return env != nil && started
}
