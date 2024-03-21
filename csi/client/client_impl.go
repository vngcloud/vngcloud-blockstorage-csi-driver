package client

import (
	"github.com/cuongpiger/joat/utils"
	"github.com/vngcloud/vngcloud-go-sdk/client"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud"

	"k8s.io/klog/v2"
)

func LogCfg(authOpts AuthOpts) {
	// If not comment, those fields are empty
	klog.V(5).Infof("Identity-URL: %s", authOpts.IdentityURL)
	klog.V(5).Infof("vServer-URL: %s", authOpts.VServerURL)
	klog.V(5).Infof("Client-ID: %s", authOpts.ClientID)
}

func NewVContainerClient(authOpts *AuthOpts) (*client.ProviderClient, error) {
	identityUrl := utils.NormalizeURL(authOpts.IdentityURL) + "v2"
	provider, _ := vngcloud.NewClient(identityUrl)
	err := vngcloud.Authenticate(provider, authOpts.ToOAuth2Options())

	return provider, err
}
