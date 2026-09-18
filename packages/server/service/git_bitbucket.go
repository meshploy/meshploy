package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Bitbucket Cloud.
//
// Two ways in, the same two GitLab and Gitea have:
//
//   - an Atlassian API token, pasted (auth_method "pat"). Atlassian replaced app
//     passwords with scoped API tokens, and a token is not tied to a callback on
//     any host - which is also what Dokploy asks for, so a connection migrated
//     from there arrives working rather than needing to be reconnected.
//   - an OAuth consumer (auth_method "oauth"), for a connection that belongs to
//     the workspace rather than to one person's token.
//
// Cloud only. Bitbucket Data Center is a different API on the customer's own
// host, so there is no base URL to set here: every call goes to
// api.bitbucket.org, and the console offers no instance field.

// Bitbucket Cloud's two hosts. Variables rather than constants so a test can
// point them at a stub; nothing else reassigns them.
var (
	bitbucketAPI = "https://api.bitbucket.org/2.0"
	bitbucketWeb = "https://bitbucket.org"
)

const (
	// bitbucketGitUser is Atlassian's fixed user name for git over HTTPS with an
	// API token, meant for exactly this: an integration that holds a token and
	// not an account. It is why the console asks for a token and nothing else,
	// where Dokploy asks for a user name and an email as well.
	bitbucketGitUser = "x-bitbucket-api-token-auth"

	// bitbucketPageLen is the largest page Bitbucket serves.
	bitbucketPageLen = 100

	// bitbucketMaxPages stops a paginated read that never ends. 100 pages is
	// 10,000 repositories, well past any workspace that would be picked from a
	// list.
	bitbucketMaxPages = 100
)

// bitbucketAuthorizeURL is where the operator approves an OAuth consumer.
//
// No scope parameter: on Bitbucket the scopes belong to the consumer and are
// chosen when it is created, unlike GitLab and Gitea where the app asks for
// them per request.
func bitbucketAuthorizeURL(clientID, redirectURI, state string) string {
	return fmt.Sprintf("%s/site/oauth2/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s",
		bitbucketWeb, url.QueryEscape(clientID), url.QueryEscape(redirectURI), state)
}

// bitbucketExchangeCode turns an authorization code into a token pair.
//
// Bitbucket's access tokens last two hours, so the refresh token matters more
// here than anywhere else; resolveOAuthToken renews from it.
func bitbucketExchangeCode(clientID, clientSecret, code, redirectURI string) (oauthTokenResult, error) {
	return bitbucketTokenRequest(clientID, clientSecret, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
}

// bitbucketRefreshToken renews an access token.
func bitbucketRefreshToken(clientID, clientSecret, refreshToken string) (oauthTokenResult, error) {
	return bitbucketTokenRequest(clientID, clientSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

// bitbucketTokenRequest posts to the token endpoint with the client credentials
// as HTTP Basic.
//
// That is the one place Bitbucket differs from GitLab and Gitea, which take the
// credentials as form fields - which is why this does not go through
// doTokenRequest: a provider's quirk should not reach the path the other two
// are on.
func bitbucketTokenRequest(clientID, clientSecret string, body url.Values) (oauthTokenResult, error) {
	req, _ := http.NewRequest(http.MethodPost, bitbucketWeb+"/site/oauth2/access_token",
		strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthTokenResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return oauthTokenResult{}, fmt.Errorf("Bitbucket token endpoint returned %d: %s", resp.StatusCode, string(raw))
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return oauthTokenResult{}, fmt.Errorf("decode token response: %w", err)
	}
	if result.Error != "" {
		return oauthTokenResult{}, fmt.Errorf("OAuth error: %s", result.Error)
	}
	if result.AccessToken == "" {
		return oauthTokenResult{}, fmt.Errorf("empty access token in response")
	}
	var expiry *time.Time
	if result.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
		expiry = &t
	}
	return oauthTokenResult{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, Expiry: expiry}, nil
}

// listBitbucketRepos lists what the token can see. workspace narrows it, the
// way Groups does for GitLab and Gitea; empty lists every workspace the account
// is a member of.
func listBitbucketRepos(token, workspace string) ([]GitRepo, error) {
	next := fmt.Sprintf("%s/repositories?role=member&pagelen=%d&sort=-updated_on", bitbucketAPI, bitbucketPageLen)
	if workspace != "" {
		next = fmt.Sprintf("%s/repositories/%s?pagelen=%d&sort=-updated_on",
			bitbucketAPI, url.PathEscape(workspace), bitbucketPageLen)
	}

	var all []GitRepo
	for pages := 0; next != "" && pages < bitbucketMaxPages; pages++ {
		var page struct {
			Values []struct {
				FullName   string `json:"full_name"`
				IsPrivate  bool   `json:"is_private"`
				MainBranch struct {
					Name string `json:"name"`
				} `json:"mainbranch"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if err := bitbucketGet(next, token, &page); err != nil {
			return nil, err
		}
		for _, r := range page.Values {
			all = append(all, GitRepo{
				FullName:      r.FullName,
				DefaultBranch: r.MainBranch.Name,
				Private:       r.IsPrivate,
			})
		}
		next = page.Next
	}
	return all, nil
}

// listBitbucketBranches lists branch names for workspace/repo.
func listBitbucketBranches(token, repo string) ([]string, error) {
	next := fmt.Sprintf("%s/repositories/%s/refs/branches?pagelen=%d", bitbucketAPI, repo, bitbucketPageLen)

	var all []string
	for pages := 0; next != "" && pages < bitbucketMaxPages; pages++ {
		var page struct {
			Values []struct {
				Name string `json:"name"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if err := bitbucketGet(next, token, &page); err != nil {
			return nil, err
		}
		for _, b := range page.Values {
			all = append(all, b.Name)
		}
		next = page.Next
	}
	return all, nil
}

// bitbucketGet reads one page.
//
// Bitbucket pages by returning the next page's whole URL rather than a number,
// so callers follow `next` instead of counting.
func bitbucketGet(apiURL, token string, out any) error {
	req, _ := http.NewRequest(http.MethodGet, apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// The caller refreshes an OAuth token once and retries; for an API
		// token it becomes "reconnect this integration".
		return errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Bitbucket API returned %d: %s", resp.StatusCode, string(raw))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
