package cosign

import (
	"context"
	"crypto"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/in-toto/in-toto-golang/in_toto"
	"github.com/kyverno/kyverno/pkg/engine/common"
	"github.com/sigstore/cosign/cmd/cosign/cli/fulcio"
	"github.com/sigstore/cosign/pkg/cosign/attestation"
	"github.com/sigstore/sigstore/pkg/signature/dsse"
	"strings"

	"github.com/gardener/controller-manager-library/pkg/logger"
	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pkg/errors"
	"github.com/sigstore/cosign/pkg/cosign"
	"github.com/sigstore/sigstore/pkg/signature"
	"k8s.io/client-go/kubernetes"
)

var (
	// ImageSignatureRepository is an alternate signature repository
	ImageSignatureRepository string
)

// Initialize loads the image pull secrets and initializes the default auth method for container registry API calls
func Initialize(client kubernetes.Interface, namespace, serviceAccount string, imagePullSecrets []string) error {
	var kc authn.Keychain
	kcOpts := &k8schain.Options{
		Namespace:          namespace,
		ServiceAccountName: serviceAccount,
		ImagePullSecrets:   imagePullSecrets,
	}

	kc, err := k8schain.New(context.Background(), client, *kcOpts)
	if err != nil {
		return errors.Wrap(err, "failed to initialize registry keychain")
	}

	authn.DefaultKeychain = kc
	return nil
}

func VerifySignature(imageRef string, key []byte, repository string, log logr.Logger) (digest string, err error) {
	pubKey, err := decodePEM(key)
	if err != nil {
		return "", errors.Wrapf(err, "failed to decode PEM %v", string(key))
	}

	cosignOpts := &cosign.CheckOpts{
		RootCerts:   fulcio.GetRoots(),
		Annotations: map[string]interface{}{},
		SigVerifier: pubKey,
		RegistryClientOpts: []remote.Option{
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
		},
	}

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse image")
	}

	cosignOpts.SignatureRepo = ref.Context()
	if repository != "" {
		signatureRepo, err := name.NewRepository(repository)
		if err != nil {
			return "", errors.Wrapf(err, "failed to parse signature repository %s", repository)
		}

		cosignOpts.SignatureRepo = signatureRepo
	}

	verified, err := client.Verify(context.Background(), ref, cosignOpts)
	if err != nil {
		msg := err.Error()
		logger.Info("image verification failed", "error", msg)
		if strings.Contains(msg, "MANIFEST_UNKNOWN: manifest unknown") {
			return "", fmt.Errorf("signature not found")
		} else if strings.Contains(msg, "no matching signatures") {
			return "", fmt.Errorf("signature mismatch")
		}

		return "", err
	}

	digest, err = extractDigest(imageRef, verified, log)
	if err != nil {
		return "", errors.Wrap(err, "failed to get digest")
	}

	return digest, nil
}

// FetchAttestations retrieves signed attestations and decodes them into in-toto statements
// https://github.com/in-toto/attestation/blob/main/spec/README.md#statement
func FetchAttestations(imageRef string, key []byte, repository string) ([]map[string]interface{}, error) {
	pubKey, err := decodePEM(key)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to decode PEM %v", string(key))
	}

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse image")
	}

	cosignOpts := &cosign.CheckOpts{
		//RootCerts:            fulcio.GetRoots(),
		ClaimVerifier:        cosign.IntotoSubjectClaimVerifier,
		SigTagSuffixOverride: cosign.AttestationTagSuffix,
		SigVerifier:          dsse.WrapVerifier(pubKey),
		VerifyBundle:         false,
	}

	if err := setSignatureRepo(cosignOpts, ref, repository); err != nil {
		return nil, errors.Wrap(err, "failed to set signature repository")
	}

	verified, err := client.Verify(context.Background(), ref, cosignOpts)
	if err != nil {
		msg := err.Error()
		logger.Info("failed to fetch attestations", "error", msg)
		if strings.Contains(msg, "MANIFEST_UNKNOWN: manifest unknown") {
			return nil, fmt.Errorf("not found")
		}

		return nil, err
	}

	inTotoStatements, err := decodeStatements(verified)
	if err != nil {
		return nil, err
	}

	return inTotoStatements, nil
}

func decodeStatements(sigs []cosign.SignedPayload) ([]map[string]interface{}, error) {
	if len(sigs) == 0 {
		return []map[string]interface{}{}, nil
	}

	decodedStatements := make([]map[string]interface{}, len(sigs))
	for i, sig := range sigs {
		data := make(map[string]interface{})
		if err := json.Unmarshal(sig.Payload, &data); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal JSON payload: %v", sig)
		}

		payloadBase64 := data["payload"].(string)
		decodedStatement, err := decodeStatement(payloadBase64)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to decode statement %s", payloadBase64)
		}

		decodedStatements[i] = decodedStatement
	}

	return decodedStatements, nil
}

func decodeStatement(payloadBase64 string) (map[string]interface{}, error) {
	statementRaw, err := base64.StdEncoding.DecodeString(payloadBase64)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to base64 decode payload for %v", statementRaw)
	}

	var statement in_toto.Statement
	if err := json.Unmarshal(statementRaw, &statement); err != nil {
		return nil, err
	}

	if statement.PredicateType != attestation.CosignCustomProvenanceV01 {
		// This assumes that the following statements are JSON objects:
		// - in_toto.PredicateSLSAProvenanceV01
		// - in_toto.PredicateLinkV1
		// - in_toto.PredicateSPDX
		// any other custom predicate
		return common.ToMap(statement)
	}

	return decodeCosignCustomProvenanceV01(statement)
}

func decodeCosignCustomProvenanceV01(statement in_toto.Statement) (map[string]interface{}, error) {
	if statement.PredicateType != attestation.CosignCustomProvenanceV01 {
		return nil, fmt.Errorf("invalid statement type %s", attestation.CosignCustomProvenanceV01)
	}

	predicate, ok := statement.Predicate.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("failed to decode CosignCustomProvenanceV01")
	}

	cosignPredicateData := predicate["Data"]
	if cosignPredicateData == nil {
		return nil, fmt.Errorf("missing predicate in CosignCustomProvenanceV01")
	}

	// attempt to parse as a JSON object type
	data, err := stringToJSONMap(cosignPredicateData)
	if err == nil {
		predicate["Data"] = data
		statement.Predicate = predicate
	}

	return common.ToMap(statement)
}

func stringToJSONMap(i interface{}) (map[string]interface{}, error) {
	s, ok := i.(string)
	if !ok {
		return nil, fmt.Errorf("expected string type")
	}

	var data = map[string]interface{}{}
	if err := json.Unmarshal([]byte(s), &data); err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %s", err.Error())
	}

	return data, nil
}

func setSignatureRepo(cosignOpts *cosign.CheckOpts, ref name.Reference, repository string) error {
	cosignOpts.SignatureRepo = ref.Context()
	if repository != "" {
		signatureRepo, err := name.NewRepository(repository)
		if err != nil {
			return errors.Wrapf(err, "failed to parse signature repository %s", repository)
		}

		cosignOpts.SignatureRepo = signatureRepo
	}
	return nil
}

func decodePEM(raw []byte) (signature.Verifier, error) {
	// PEM encoded file.
	ed, err := cosign.PemToECDSAKey(raw)
	if err != nil {
		return nil, errors.Wrap(err, "pem to ecdsa")
	}

	return signature.LoadECDSAVerifier(ed, crypto.SHA256)
}

func extractDigest(imgRef string, verified []cosign.SignedPayload, log logr.Logger) (string, error) {
	var jsonMap map[string]interface{}
	for _, vp := range verified {
		if err := json.Unmarshal(vp.Payload, &jsonMap); err != nil {
			return "", err
		}

		log.V(4).Info("image verification response", "image", imgRef, "payload", jsonMap)

		// The cosign response is in the JSON format:
		// {
		//   "critical": {
		// 	   "identity": {
		// 	     "docker-reference": "registry-v2.nirmata.io/pause"
		// 	    },
		//   	"image": {
		// 	     "docker-manifest-digest": "sha256:4a1c4b21597c1b4415bdbecb28a3296c6b5e23ca4f9feeb599860a1dac6a0108"
		// 	    },
		// 	    "type": "cosign container image signature"
		//    },
		//    "optional": null
		// }
		critical := jsonMap["critical"].(map[string]interface{})
		if critical != nil {
			typeStr := critical["type"].(string)
			if typeStr == "cosign container image signature" {
				identity := critical["identity"].(map[string]interface{})
				if identity != nil {
					image := critical["image"].(map[string]interface{})
					if image != nil {
						return image["docker-manifest-digest"].(string), nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("digest not found for " + imgRef)
}
