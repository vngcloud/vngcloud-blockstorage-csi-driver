package client

import (
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/extensions/oauth2"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/identity/v2/tokens"
)

type (
	AuthOpts struct {
		IdentityURL  string `gcfg:"identity-url" mapstructure:"identity-url" name:"identity-url"`
		VServerURL   string `gcfg:"vserver-url" mapstructure:"vserver-url" name:"vserver-url"`
		ClientID     string `gcfg:"client-id" mapstructure:"client-id" name:"client-id"`
		ClientSecret string `gcfg:"client-secret" mapstructure:"client-secret" name:"client-secret"`
	}
)

func (s *AuthOpts) ToOAuth2Options() *oauth2.AuthOptions {
	return &oauth2.AuthOptions{
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		AuthOptionsBuilder: &tokens.AuthOptions{
			IdentityEndpoint: s.IdentityURL,
		},
	}
}
