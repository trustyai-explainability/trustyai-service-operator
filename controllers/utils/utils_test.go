package utils

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Utils Suite")
}

var _ = Describe("GetNamespace", func() {
	var (
		originalEnvValue   string
		originalSAFilePath string
		tempDir            string
	)

	BeforeEach(func() {
		// Save original values
		originalEnvValue = os.Getenv("APPLICATIONS_NAMESPACE")
		originalSAFilePath = serviceAccountNamespaceFile

		// Clean up env var
		Expect(os.Unsetenv("APPLICATIONS_NAMESPACE")).To(Succeed())
	})

	AfterEach(func() {
		// Restore original env var
		if originalEnvValue != "" {
			Expect(os.Setenv("APPLICATIONS_NAMESPACE", originalEnvValue)).To(Succeed())
		} else {
			Expect(os.Unsetenv("APPLICATIONS_NAMESPACE")).To(Succeed())
		}

		// Restore original service account file path
		serviceAccountNamespaceFile = originalSAFilePath

		// Clean up temp directory if created
		if tempDir != "" {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
			tempDir = ""
		}
	})

	Context("when APPLICATIONS_NAMESPACE environment variable is set", func() {
		It("should return the environment variable value", func() {
			expectedNS := "platform-injected-namespace"
			Expect(os.Setenv("APPLICATIONS_NAMESPACE", expectedNS)).To(Succeed())

			ns, err := GetNamespace()

			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal(expectedNS))
		})

		It("should prefer environment variable over service account file", func() {
			// Set env var
			envNS := "env-var-namespace"
			Expect(os.Setenv("APPLICATIONS_NAMESPACE", envNS)).To(Succeed())

			// The actual service account file doesn't matter since env var takes precedence
			ns, err := GetNamespace()

			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal(envNS))
		})
	})

	Context("when APPLICATIONS_NAMESPACE environment variable is empty", func() {
		It("should fall back to service account file", func() {
			// Verify env var is unset so fallback logic would be triggered
			Expect(os.Getenv("APPLICATIONS_NAMESPACE")).To(BeEmpty())

			// Create a temporary service account namespace file
			var err error
			tempDir, err = os.MkdirTemp("", "sa-namespace-test")
			Expect(err).NotTo(HaveOccurred())

			tempFile := filepath.Join(tempDir, "namespace")
			expectedNS := "service-account-namespace"
			Expect(os.WriteFile(tempFile, []byte(expectedNS), 0644)).To(Succeed())

			// Point GetNamespace to our test file
			serviceAccountNamespaceFile = tempFile

			// Call GetNamespace and verify it reads from the file
			ns, err := GetNamespace()

			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal(expectedNS), "should read namespace from service account file when env var is empty")
		})
	})

	Context("environment variable precedence", func() {
		It("should use env var when both env var and service account file exist", func() {
			envNS := "platform-namespace"
			Expect(os.Setenv("APPLICATIONS_NAMESPACE", envNS)).To(Succeed())

			ns, err := GetNamespace()

			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal(envNS), "should use APPLICATIONS_NAMESPACE env var, not service account file")
		})
	})
})
