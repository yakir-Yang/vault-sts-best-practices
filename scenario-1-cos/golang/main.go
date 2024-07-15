package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	sts "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts/v20180813"
	"github.com/tencentyun/cos-go-sdk-v5"
)

type Response struct {

	Credentials *sts.Credentials `json:"credentials"`
	CosBuckets  []cos.Bucket     `json:"cos_buckets"`
}

func assumeRoleWithWebIdentity() (*sts.Credentials, error) {
	region := os.Getenv("TKE_REGION")
				
	if region == "" {
		return nil, errors.New("env TKE_REGION not exist")
	}

	providerId := os.Getenv("TKE_PROVIDER_ID")
	if providerId == "" {
		fmt.Println("env TKE_PROVIDER_ID not exist")
		return nil, errors.New("env TKE_PROVIDER_ID not exist")
	}

	tokenFile := os.Getenv("TKE_WEB_IDENTITY_TOKEN_FILE")
	if tokenFile == "" {
		return nil, errors.New("env TKE_WEB_IDENTITY_TOKEN_FILE not exist")
	}
	tokenBytes, err := ioutil.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}
	webIdentityToken := string(tokenBytes)

	roleArn := os.Getenv("TKE_ROLE_ARN")
	if roleArn == "" {
		return nil, errors.New("env TKE_ROLE_ARN not exist")
	}

	sessionName := "golang-test" + strconv.FormatInt(time.Now().UnixNano()/1000, 10)

	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "sts.tencentcloudapi.com"
	cpf.HttpProfile.ReqMethod = "POST"

	client, _ := sts.NewClient(nil, region, cpf)

	request := sts.NewAssumeRoleWithWebIdentityRequest()
	request.ProviderId = common.StringPtr(providerId)
	request.WebIdentityToken = common.StringPtr(webIdentityToken)
	request.RoleArn = common.StringPtr(roleArn)
	request.RoleSessionName = common.StringPtr(sessionName)
	request.DurationSeconds = common.Int64Ptr(3600)

	response, err := client.AssumeRoleWithWebIdentity(request)
	if err != nil {
		log.Printf("Failed to assume role with web identity: %v", err)
		return nil, err
	}

	return response.Response.Credentials, nil
}

func listCosBuckets(credentials *sts.Credentials) ([]cos.Bucket, error) {
	u, _ := url.Parse("https://cos.ap-jakarta.myqcloud.com")
	b := &cos.BaseURL{BucketURL: u}

	credential := common.NewTokenCredential(
		*credentials.TmpSecretId,
		*credentials.TmpSecretKey,
		*credentials.Token,
	)

	client := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:     credential.SecretId,
			SecretKey:    credential.SecretKey,
			SessionToken: credential.Token,
		},
	})

	result, _, err := client.Service.Get(context.Background())
	if err != nil {
		log.Printf("Failed to list COS buckets: %v", err)
		return nil, err
	}

	return result.Buckets, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	credentials, err := assumeRoleWithWebIdentity()
	if err != nil {
		http.Error(w, "Failed to assume role with web identity", http.StatusInternalServerError)
		return
	}

	cosBuckets, err := listCosBuckets(credentials)
	if err != nil {
		http.Error(w, "Failed to list COS buckets", http.StatusInternalServerError)
		return
	}

	response := Response{
		Credentials: credentials,
		CosBuckets:  cosBuckets,
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

type oidcStsRsp struct {
	Response struct {
		Credentials struct {
			Token        string `json:"Token"`
			TmpSecretId  string `json:"TmpSecretId"`
			TmpSecretKey string `json:"TmpSecretKey"`
		} `json:"Credentials"`
		ExpiredTime int       `json:"ExpiredTime"`
		Expiration  time.Time `json:"Expiration"`
		RequestId   string    `json:"RequestId"`
	} `json:"Response"`
}

func main() {
	http.HandleFunc("/", handler)
	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
