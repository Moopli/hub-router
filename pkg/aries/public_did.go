/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package aries

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/hyperledger/aries-framework-go-ext/component/vdr/orb"
	"github.com/hyperledger/aries-framework-go/pkg/doc/did"
	"github.com/hyperledger/aries-framework-go/pkg/doc/util/jwkkid"
	vdrapi "github.com/hyperledger/aries-framework-go/pkg/framework/aries/api/vdr"
	"github.com/hyperledger/aries-framework-go/pkg/kms"
	"github.com/hyperledger/aries-framework-go/spi/storage"
)

const (
	storeName   = "router-invitation-did"
	storeDIDKey = "did-value"
)

// PublicDIDGetter initializes and provides the public DID this router will use.
type PublicDIDGetter struct {
	ctx        Ctx
	httpClient *http.Client
	store      storage.Store
}

// PublicDIDConfig contains parameters for Orb public DID creation.
type PublicDIDConfig struct {
	TLSConfig             *tls.Config
	OrbDomain             string
	OrbAnchorOrigin       string
	OrbOperationEndpoints []string
	DIDCommEndPoint       string
}

// GetPublicDID gets the public DID that this router will use for OOBv2 invitations.
func GetPublicDID(ctx Ctx, cfg *PublicDIDConfig) (string, error) {
	pdg, err := newPublicDIDGetter(ctx, cfg.TLSConfig)
	if err != nil {
		return "", err
	}

	return pdg.Initialize(cfg.OrbDomain, cfg.OrbAnchorOrigin, cfg.OrbOperationEndpoints, cfg.DIDCommEndPoint)
}

// newPublicDIDGetter returns a new PublicDIDGetter.
func newPublicDIDGetter(ctx Ctx, tlsConfig *tls.Config) (*PublicDIDGetter, error) {
	store, err := ctx.StorageProvider().OpenStore(storeName)
	if err != nil {
		return nil, fmt.Errorf("open invitation DID store: %w", err)
	}

	return &PublicDIDGetter{
		ctx:        ctx,
		store:      store,
		httpClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
	}, nil
}

// Initialize initializes the PublicDIDGetter by creating a public orb DID.
func (g *PublicDIDGetter) Initialize(orbDomain, orbAnchorOrigin string, orbOperationEndpoints []string,
	didcommEndPoint string) (string, error) {
	res, err := g.store.Get(storeDIDKey)
	if err == nil {
		// another router instance has created the public DID and saved to a shared/persistent store.
		return string(res), nil
	}

	didDoc, err := g.docTemplate(didcommEndPoint)
	if err != nil {
		return "", err
	}

	docRes, err := g.requestOrbCreate(didDoc, orbDomain, orbAnchorOrigin, orbOperationEndpoints)
	if err != nil {
		return "", fmt.Errorf("creating public orb DID: %w", err)
	}

	err = g.store.Put(storeDIDKey, []byte(docRes.DIDDocument.ID))
	if err != nil {
		return "", fmt.Errorf("error saving public DID: %w", err)
	}

	return docRes.DIDDocument.ID, nil
}

func (g *PublicDIDGetter) docTemplate(didcommEndPoint string) (*did.Doc, error) {
	didDoc := did.Doc{}

	auth, err := g.createVerification("#key-1", g.ctx.KeyType(), did.Authentication)
	if err != nil {
		return nil, fmt.Errorf("creating did doc Authentication: %w", err)
	}

	didDoc.Authentication = append(didDoc.Authentication, *auth)

	kagr, err := g.createVerification("#key-2", g.ctx.KeyAgreementType(), did.KeyAgreement)
	if err != nil {
		return nil, fmt.Errorf("creating did doc KeyAgreement: %w", err)
	}

	didDoc.KeyAgreement = append(didDoc.KeyAgreement, *kagr)

	didDoc.Service = []did.Service{{
		ID:              uuid.New().String(),
		ServiceEndpoint: didcommEndPoint,
		Type:            "DIDCommMessaging",
	}}

	return &didDoc, nil
}

func (g *PublicDIDGetter) requestOrbCreate(
	doc *did.Doc, orbDomain, orbAnchorOrigin string, orbOperationEndpoints []string) (*did.DocResolution, error) {
	publicKeyRecovery, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	publicKeyUpdate, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	orbOpts := []orb.Option{
		orb.WithHTTPClient(g.httpClient),
		orb.WithDomain(orbDomain),
	}

	vdr, err := orb.New(nil, orbOpts...)
	if err != nil {
		return nil, err
	}

	createOpts := []vdrapi.DIDMethodOption{
		vdrapi.WithOption(orb.AnchorOriginOpt, orbAnchorOrigin),
		vdrapi.WithOption(orb.UpdatePublicKeyOpt, publicKeyUpdate),
		vdrapi.WithOption(orb.RecoveryPublicKeyOpt, publicKeyRecovery),
	}

	if len(orbOperationEndpoints) > 0 {
		createOpts = append(createOpts, vdrapi.WithOption(orb.OperationEndpointsOpt, orbOperationEndpoints))
	}

	return vdr.Create(doc, createOpts...)
}

func (g *PublicDIDGetter) createVerification(id string, kt kms.KeyType, relationship did.VerificationRelationship,
) (*did.Verification, error) {
	kid, pkBytes, err := g.ctx.KMS().CreateAndExportPubKeyBytes(kt)
	if err != nil {
		return nil, fmt.Errorf("creating public key: %w", err)
	}

	j, err := jwkkid.BuildJWK(pkBytes, kt)
	if err != nil {
		return nil, fmt.Errorf("creating jwk: %w", err)
	}

	j.KeyID = kid

	vm, err := did.NewVerificationMethodFromJWK(id, "JsonWebKey2020", "", j)
	if err != nil {
		return nil, fmt.Errorf("creating verification method: %w", err)
	}

	return did.NewReferencedVerification(vm, relationship), nil
}
