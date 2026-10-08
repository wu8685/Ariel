package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository from test source")
	}
	return filepath.Join(filepath.Dir(current), "..", "..")
}

func TestKubernetesBaseDefinesSingleSecureRelayPod(t *testing.T) {
	deployment := repositoryFile(t, "deploy/kubernetes/base/deployment.yaml")
	for _, required := range []string{
		"replicas: 1",
		"type: Recreate",
		"image: ariel-relay:local",
		"containerPort: 8080",
		"startupProbe:",
		"readinessProbe:",
		"livenessProbe:",
		"path: /healthz",
		"runAsNonRoot: true",
		"runAsUser: 10001",
		"readOnlyRootFilesystem: true",
		"allowPrivilegeEscalation: false",
		"seccompProfile:",
		"type: RuntimeDefault",
		"automountServiceAccountToken: false",
		"configMapRef:",
		"secretRef:",
		"name: ariel-relay-secrets",
		"requests:",
		"limits:",
	} {
		if !strings.Contains(deployment, required) {
			t.Errorf("base Deployment missing %q", required)
		}
	}
	service := repositoryFile(t, "deploy/kubernetes/base/service.yaml")
	for _, required := range []string{"type: ClusterIP", "port: 80", "targetPort: http"} {
		if !strings.Contains(service, required) {
			t.Errorf("base Service missing %q", required)
		}
	}
}

func TestKubernetesPINOverlayStaysOnTrustedNetworkWithoutPersistence(t *testing.T) {
	config := repositoryFile(t, "deploy/kubernetes/overlays/pin/configmap.yaml")
	for _, required := range []string{"ARIEL_WEB_AUTH: pin", "ARIEL_ORIGINS: http://ariel.lan"} {
		if !strings.Contains(config, required) {
			t.Errorf("PIN ConfigMap missing %q", required)
		}
	}
	kustomization := repositoryFile(t, "deploy/kubernetes/overlays/pin/kustomization.yaml")
	if strings.Contains(kustomization, "pvc") || strings.Contains(kustomization, "deployment-patch") {
		t.Fatal("PIN overlay must not add Passkey persistence")
	}
	ingress := repositoryFile(t, "deploy/kubernetes/overlays/pin/ingress.yaml")
	if !strings.Contains(ingress, "host: ariel.lan") || strings.Contains(ingress, "tls:") {
		t.Fatal("PIN example must be an explicitly non-public trusted-network ingress")
	}
}

func TestKubernetesPasskeyOverlayPersistsCredentialsBehindTLS(t *testing.T) {
	config := repositoryFile(t, "deploy/kubernetes/overlays/passkey/configmap.yaml")
	for _, required := range []string{
		"ARIEL_WEB_AUTH: passkey",
		"ARIEL_ORIGINS: https://ariel.example.com",
		"ARIEL_PUBLIC_ORIGIN: https://ariel.example.com",
		"ARIEL_WEBAUTHN_RP_ID: ariel.example.com",
		"ARIEL_WEBAUTHN_CREDENTIALS_FILE: /data/auth.json",
	} {
		if !strings.Contains(config, required) {
			t.Errorf("Passkey ConfigMap missing %q", required)
		}
	}
	patch := repositoryFile(t, "deploy/kubernetes/overlays/passkey/deployment-patch.yaml")
	for _, required := range []string{"mountPath: /data", "claimName: ariel-auth"} {
		if !strings.Contains(patch, required) {
			t.Errorf("Passkey Deployment patch missing %q", required)
		}
	}
	pvc := repositoryFile(t, "deploy/kubernetes/overlays/passkey/pvc.yaml")
	if !strings.Contains(pvc, "ReadWriteOnce") {
		t.Fatal("Passkey PVC must use ReadWriteOnce")
	}
	ingress := repositoryFile(t, "deploy/kubernetes/overlays/passkey/ingress.yaml")
	for _, required := range []string{"host: ariel.example.com", "secretName: ariel-tls", "tls:"} {
		if !strings.Contains(ingress, required) {
			t.Errorf("Passkey Ingress missing %q", required)
		}
	}
}

func TestKubernetesManifestsNeverPersistSecretObjects(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "deploy", "kubernetes")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(raw)
		for _, forbidden := range []string{"kind: Secret", "stringData:", "data:\n  ARIEL_TOKEN"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s persists forbidden secret material %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestKubernetesGuideDocumentsBothAuthenticationModes(t *testing.T) {
	doc := repositoryFile(t, "docs/operations/kubernetes-deployment.md")
	for _, required := range []string{
		"kubectl apply -k deploy/kubernetes/overlays/pin",
		"kubectl apply -k deploy/kubernetes/overlays/passkey",
		"kubectl create secret generic ariel-relay-secrets",
		"不可变 tag 或 digest",
		"Desktop Agent",
		"不要扩容",
		"ARIEL_PASSKEY_SETUP_TOKEN",
		"ARIEL_SESSION_KEY",
		"ariel-tls",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("Kubernetes guide missing %q", required)
		}
	}
}
