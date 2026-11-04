package tlsprofile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func TestReadTLSProfileStateParsesGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/config.openshift.io/v1":
			_ = json.NewEncoder(w).Encode(&metav1.APIResourceList{
				GroupVersion: "config.openshift.io/v1",
				APIResources: []metav1.APIResource{{
					Name: "apiservers", Kind: "APIServer", Verbs: metav1.Verbs{"get"},
				}},
			})
		case "/apis/config.openshift.io/v1/apiservers/cluster":
			_, _ = w.Write([]byte(`{
				"apiVersion":"config.openshift.io/v1",
				"kind":"APIServer",
				"metadata":{"name":"cluster"},
				"spec":{
					"tlsSecurityProfile":{
						"type":"Custom",
						"custom":{
							"minTLSVersion":"VersionTLS13",
							"groups":["X25519","secp256r1"]
						}
					},
					"tlsAdherence":"StrictAllComponents"
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &rest.Config{Host: server.URL}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("dynamic.NewForConfig() error = %v", err)
	}
	state, err := readTLSProfileState(context.Background(), config, client)
	if err != nil {
		t.Fatalf("readTLSProfileState() error = %v", err)
	}
	want := []configv1.TLSGroup{configv1.TLSGroupX25519, configv1.TLSGroupSecP256r1}
	if got := state.profile.Custom.Groups; !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed TLS groups = %v, want %v", got, want)
	}
}
