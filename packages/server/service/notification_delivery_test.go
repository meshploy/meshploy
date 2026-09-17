package service_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A dispatch that fails used to leave one line in the API log and nothing the
// console could show. Every attempt is now recorded, a channel reports how
// many in a row have failed, and a failed one can be sent again.
func TestDeliveriesAreRecordedAndRetried(t *testing.T) {
	db := newTestDB(t)
	svcs := newServices(db)
	ctx := context.Background()

	var up atomic.Bool
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !up.Load() {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()

	org := meshdb.Organization{Name: "acme", Slug: "acme"}
	require.NoError(t, db.Create(&org).Error)
	ch, err := svcs.Notifications.Create(ctx, org.ID, service.CreateNotificationInput{
		Name: "hook", Type: meshdb.NotificationWebhook,
		Config: map[string]string{"url": srv.URL}, Events: []string{"deploy.failed"},
	})
	require.NoError(t, err)

	data := service.NotificationData{ServiceName: "api", ProjectName: "shop"}
	svcs.Notifications.Dispatch(ctx, org.ID, "deploy.failed", data)
	svcs.Notifications.Dispatch(ctx, org.ID, "deploy.failed", data)
	svcs.Notifications.Dispatch(ctx, org.ID, "deploy.success", data) // not subscribed

	failed, err := svcs.Notifications.Deliveries(ctx, org.ID, ch.ID, true, 50)
	require.NoError(t, err)
	require.Len(t, failed, 2)
	assert.Equal(t, "HTTP 502", failed[0].Error)
	assert.Equal(t, "api", failed[0].Data["service"])

	list, err := svcs.Notifications.List(ctx, org.ID)
	require.NoError(t, err)
	require.NotNil(t, list[0].LastDelivery)
	assert.Equal(t, 2, list[0].FailingStreak)

	up.Store(true)
	retried, err := svcs.Notifications.Retry(ctx, org.ID, failed[0].ID)
	require.NoError(t, err)
	assert.True(t, retried.Success)
	assert.Equal(t, failed[0].ID, *retried.RetryOf)
	assert.Equal(t, "deploy.failed", retried.Event)

	list, err = svcs.Notifications.List(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, list[0].FailingStreak)

	test, err := svcs.Notifications.Test(ctx, org.ID, ch.ID)
	require.NoError(t, err)
	assert.True(t, test.Success)
	assert.True(t, test.Test)
	assert.Equal(t, int32(4), hits.Load())

	// Another org can neither read nor retry this channel's attempts.
	other := meshdb.Organization{Name: "other", Slug: "other"}
	require.NoError(t, db.Create(&other).Error)
	_, err = svcs.Notifications.Deliveries(ctx, other.ID, ch.ID, false, 50)
	assert.Error(t, err)
	_, err = svcs.Notifications.Retry(ctx, other.ID, failed[0].ID)
	assert.Error(t, err)

	// Deleting the channel takes its log with it.
	require.NoError(t, svcs.Notifications.Delete(ctx, ch.ID, org.ID))
	var n int64
	db.Model(&meshdb.NotificationDelivery{}).Count(&n)
	assert.Zero(t, n)
}

// A Slack or Discord webhook URL is the credential, and Go's HTTP errors quote
// the URL, so the stored error must not.
func TestDeliveryErrorLeavesOutTheWebhookURL(t *testing.T) {
	db := newTestDB(t)
	svcs := newServices(db)
	ctx := context.Background()

	org := meshdb.Organization{Name: "acme", Slug: "acme"}
	require.NoError(t, db.Create(&org).Error)
	secretURL := "http://127.0.0.1:1/services/T000/B000/sekrit"
	ch, err := svcs.Notifications.Create(ctx, org.ID, service.CreateNotificationInput{
		Name: "slack", Type: meshdb.NotificationSlack,
		Config: map[string]string{"webhook_url": secretURL}, Events: []string{"deploy.failed"},
	})
	require.NoError(t, err)

	d, err := svcs.Notifications.Test(ctx, org.ID, ch.ID)
	require.NoError(t, err)
	assert.False(t, d.Success)
	assert.NotEmpty(t, d.Error)
	assert.NotContains(t, d.Error, "sekrit")
}

// The address goes into the To header, so a line break must not get in.
func TestEmailRecipientCannotAddHeaders(t *testing.T) {
	db := newTestDB(t)
	svcs := newServices(db)
	ctx := context.Background()
	org := meshdb.Organization{Name: "acme", Slug: "acme"}
	require.NoError(t, db.Create(&org).Error)

	for _, addr := range []string{"ops@x.io\r\nBcc: evil@y.io", "not an address"} {
		_, err := svcs.Notifications.Create(ctx, org.ID, service.CreateNotificationInput{
			Name: "mail", Type: meshdb.NotificationEmail,
			Config: map[string]string{"address": addr}, Events: []string{"deploy.failed"},
		})
		assert.Error(t, err, addr)
		err = svcs.Notifications.TestEmailProvider(ctx, org.ID, addr)
		assert.Error(t, err, addr)
	}

	err := svcs.Notifications.TestEmailProvider(ctx, org.ID, "ops@x.io")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "no email provider"), err.Error())
	_, err = svcs.Notifications.Test(ctx, org.ID, uuid.New())
	assert.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled))
}
