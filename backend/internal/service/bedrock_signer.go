package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// BedrockSigner
type BedrockSigner struct {
	credentials aws.Credentials
	region      string
	signer      *v4.Signer
}

// NewBedrockSigner
func NewBedrockSigner(accessKeyID, secretAccessKey, sessionToken, region string) *BedrockSigner {
	return &BedrockSigner{
		credentials: aws.Credentials{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			SessionToken:    sessionToken,
		},
		region: region,
		signer: v4.NewSigner(),
	}
}

// NewBedrockSignerFromAccount
func NewBedrockSignerFromAccount(account *Account) (*BedrockSigner, error) {
	accessKeyID := account.GetCredential("aws_access_key_id")
	if accessKeyID == "" {
		return nil, fmt.Errorf("aws_access_key_id not found in credentials")
	}
	secretAccessKey := account.GetCredential("aws_secret_access_key")
	if secretAccessKey == "" {
		return nil, fmt.Errorf("aws_secret_access_key not found in credentials")
	}
	region := account.GetCredential("aws_region")
	if region == "" {
		region = defaultBedrockRegion
	}
	sessionToken := account.GetCredential("aws_session_token") // 可选

	return NewBedrockSigner(accessKeyID, secretAccessKey, sessionToken, region), nil
}

// SignRequest
//
//
//
//
func (s *BedrockSigner) SignRequest(ctx context.Context, req *http.Request, body []byte) error {
	payloadHash := sha256Hash(body)
	return s.signer.SignHTTP(ctx, s.credentials, req, payloadHash, "bedrock", s.region, time.Now())
}

func sha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
