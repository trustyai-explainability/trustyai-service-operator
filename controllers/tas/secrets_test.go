package tas

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("validateDatabaseSecret", func() {
	reconciler := &TrustyAIServiceReconciler{}

	It("accepts a complete database configuration", func() {
		secret := createDatabaseConfiguration("ns", "db-credentials", "mariadb", "trustyai")
		Expect(reconciler.validateDatabaseSecret(secret)).To(Succeed())
	})

	It("rejects a configuration without databaseGeneration", func() {
		secret := createDatabaseConfiguration("ns", "db-credentials", "mariadb", "trustyai")
		delete(secret.Data, "databaseGeneration")
		Expect(reconciler.validateDatabaseSecret(secret)).To(MatchError(ContainSubstring("databaseGeneration is missing")))
	})

	It("rejects a configuration with an empty databaseGeneration", func() {
		secret := createDatabaseConfiguration("ns", "db-credentials", "mariadb", "trustyai")
		secret.Data["databaseGeneration"] = []byte{}
		Expect(reconciler.validateDatabaseSecret(secret)).To(MatchError(ContainSubstring("databaseGeneration is empty")))
	})
})
