package cloud

import (
	"github.com/cuongpiger/joat/utils"
	"github.com/vngcloud/vngcloud-go-sdk/client"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/extensions/oauth2"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/tokens"
)

// NewCloud returns a new instance of AWS cloud
// It panics if session is invalid
func NewCloud(iamURL, vserverUrl, clientID, clientSecret string, metadataSvc MetadataService) (Cloud, error) {
	vserverV1 := utils.NormalizeURL(vserverUrl) + "v1"
	vserverV2 := utils.NormalizeURL(vserverUrl) + "v2"
	pc, err := newVngCloud(iamURL, clientID, clientSecret)
	if err != nil {
		return nil, err
	}

	compute, _ := vngcloud.NewServiceClient(vserverV2, pc, "compute")
	volume, _ := vngcloud.NewServiceClient(vserverV2, pc, "volume")
	portal, _ := vngcloud.NewServiceClient(vserverV1, pc, "portal")

	return &cloud{
		compute:         compute,
		volume:          volume,
		portal:          portal,
		metadataService: metadataSvc,
	}, nil
}

func newVngCloud(iamURL, clientID, clientSecret string) (*client.ProviderClient, error) {
	identityUrl := utils.NormalizeURL(iamURL) + "v2"
	provider, _ := vngcloud.NewClient(identityUrl)
	err := vngcloud.Authenticate(provider, &oauth2.AuthOptions{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthOptionsBuilder: &tokens.AuthOptions{
			IdentityEndpoint: iamURL,
		},
	})

	return provider, err
}

type (
	cloud struct {
		compute         *client.ServiceClient
		volume          *client.ServiceClient
		portal          *client.ServiceClient
		snapshot        *client.ServiceClient
		extraInfo       *extraInfa
		metadataService MetadataService
	}

	extraInfa struct {
		ProjectID string
		UserID    int64
	}
)
