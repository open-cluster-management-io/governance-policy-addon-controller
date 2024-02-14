// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"open-cluster-management.io/governance-policy-addon-controller/pkg/controllers/complianceapi"
)

var _ = Describe("Test ComplianceDBSecretReconciler", Ordered, func() {
	var routerRsrc dynamic.ResourceInterface
	var secretRsrc dynamic.ResourceInterface

	BeforeAll(func(ctx context.Context) {
		secretRsrc = clientDynamic.Resource(gvrSecret).Namespace(controllerNamespace)
		routerRsrc = clientDynamic.Resource(complianceapi.RouteGVR).Namespace(controllerNamespace)
	})

	AfterAll(func(ctx context.Context) {
		By("Deleting the " + complianceapi.DBSecretName + " secret")
		err := secretRsrc.Delete(ctx, complianceapi.DBSecretName, metav1.DeleteOptions{})
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("Creates the route when the secret is defined", func(ctx context.Context) {
		Kubectl("-n", controllerNamespace, "create", "secret", "generic", complianceapi.DBSecretName)

		var route *unstructured.Unstructured

		Eventually(func(g Gomega) {
			var err error
			route, err = routerRsrc.Get(ctx, complianceapi.RouteName, metav1.GetOptions{})
			g.Expect(err).ToNot(HaveOccurred())
		}, 30, 5).Should(Succeed())

		targetPort, _, _ := unstructured.NestedString(route.Object, "spec", "port", "targetPort")
		Expect(targetPort).To(Equal("compliance-history-api"))

		termination, _, _ := unstructured.NestedString(route.Object, "spec", "tls", "termination")
		Expect(termination).To(Equal("reencrypt"))
	})

	It("Recreates the route when the route is deleted", func(ctx context.Context) {
		err := routerRsrc.Delete(ctx, complianceapi.RouteName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func(g Gomega) {
			_, err := routerRsrc.Get(ctx, complianceapi.RouteName, metav1.GetOptions{})
			g.Expect(err).ToNot(HaveOccurred())
		}, 30, 5).Should(Succeed())
	})

	It("Deletes the route when the secret is deleted", func(ctx context.Context) {
		err := secretRsrc.Delete(ctx, complianceapi.DBSecretName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func(g Gomega) {
			_, err := routerRsrc.Get(ctx, complianceapi.RouteName, metav1.GetOptions{})
			g.Expect(k8serrors.IsNotFound(err)).To(BeTrue(), "the route was not deleted")
		}, 30, 5).Should(Succeed())
	})
})
