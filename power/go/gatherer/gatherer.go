package gatherer

// The gatherer package contains the logic that figures out which powercycle-
// enabled bots or devices are down.
// See the Gatherer interface for more details.

import (
	"sort"
	"sync"
	"time"

	swarming "github.com/luci/luci-go/common/api/swarming/swarming/v1"

	"go.skia.org/infra/go/promalertsclient"
	"go.skia.org/infra/go/sklog"
	skswarming "go.skia.org/infra/go/swarming"
	"go.skia.org/infra/go/util"
	"go.skia.org/infra/power/go/decider"
)

const (
	// Alert messages from prometheus
	ALERT_BOT_MISSING     = "BotMissing"
	ALERT_BOT_QUARANTINED = "BotQuarantined"

	// Status messages we report.
	STATUS_HOST_MISSING   = "Host Missing"
	STATUS_DEVICE_MISSING = "Device Missing"
)

// Gatherer is a simple interface around the logic behind obtaining
// a list of bots and devices that are down and could be powercycled.
type Gatherer interface {
	// DownBots returns the current set of down bots. It may be cached.
	DownBots() []DownBot
}

// DownBot represents information about a dead or quarantined bot, as well
// as the alert that is related to it.
type DownBot struct {
	BotID      string                                 `json:"bot_id"`
	Dimensions []*swarming.SwarmingRpcsStringListPair `json:"dimensions"`
	Status     string                                 `json:"status"`
	// Since represents how long the alert been firing
	Since    time.Time `json:"since"`
	Silenced bool      `json:"silenced"`
}

// The gatherer struct implements the Gatherer interface.
type gatherer struct {
	downBots []DownBot
	mutex    sync.Mutex

	iSwarming skswarming.ApiClient
	eSwarming skswarming.ApiClient
	alerts    promalertsclient.APIClient
	decider   decider.Decider
}

// NewPollingGatherer returns a Gatherer created with the given utilities. all the passed in
// clients should be properly authenticated.
func NewPollingGatherer(external, internal skswarming.ApiClient, alerts promalertsclient.APIClient, decider decider.Decider, period time.Duration) Gatherer {
	g := &gatherer{
		iSwarming: internal,
		eSwarming: external,
		alerts:    alerts,
		decider:   decider,
	}
	if period > 0 {
		go func() {
			g.update()
			for {
				<-time.Tick(period)
				g.update()
			}
		}()
	}
	return g
}

// See the Gatherer interface for more information.
func (g *gatherer) DownBots() []DownBot {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return g.downBots
}

// set writes to the underlying downBots slice in a thread safe way.
func (g *gatherer) set(bots []DownBot) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.downBots = bots
}

// downBotsFilter is a function that returns true only for alerts about
// dead and quarantined bots.
func downBotsFilter(a promalertsclient.Alert) bool {
	alertName := string(a.Labels["alertname"])
	return alertName == ALERT_BOT_MISSING || alertName == ALERT_BOT_QUARANTINED
}

// update is the "inner loop" of the gatherer. It polls swarming for a list of
// down bots. It then polls alerts for a list of down bots. It constructs the
// intersect of those lists and sets the result in g.downBots.
func (g *gatherer) update() {
	// Ask Swarming API for list of bots down in the pools we care about
	sklog.Infoln("Polling PromAlerts and Swarming API for down bots")
	bots := []*swarming.SwarmingRpcsBotInfo{}
	for _, pool := range skswarming.POOLS_PRIVATE {
		xb, err := g.iSwarming.ListDownBots(pool)
		if err != nil {
			sklog.Warningf("Could not get down bots from internal pool %s: %s", pool, err)
		}
		bots = append(bots, xb...)
	}

	for _, pool := range skswarming.POOLS_PUBLIC {
		xb, err := g.eSwarming.ListDownBots(pool)
		if err != nil {
			sklog.Warningf("Could not get down bots from external pool %s: %s", pool, err)
		}
		bots = append(bots, xb...)
	}

	if len(bots) == 0 {
		g.set([]DownBot{})
		sklog.Info("Swarming reports no down bots")
		return
	}

	sklog.Infof("Swarming reports %d down bots: %s", len(bots), bots)

	// Ask Prometheus for bot alerts related to quarantined and dead
	alerts, err := g.alerts.GetAlerts(downBotsFilter)
	if err != nil {
		sklog.Warningf("Could not get down bots from alerts %s", err)
		return
	}

	if len(alerts) == 0 {
		g.set([]DownBot{})
		sklog.Info("No bot-related alerts")
		return
	}

	sklog.Infof("Promalerts reports %d bot-related alerts: %s", len(alerts), alerts)

	// join these together to create []DownBot
	botsWithAlerts := util.StringSet{}
	alertMap := map[string]promalertsclient.Alert{}
	for _, a := range alerts {
		id := string(a.Labels["bot"])
		botsWithAlerts[id] = true
		alertMap[id] = a
	}
	botsFromSwarming := util.StringSet{}
	for _, b := range bots {
		botsFromSwarming[b.BotId] = true
	}
	matchingBots := botsWithAlerts.Intersect(botsFromSwarming)

	downBots := []DownBot{}
	for _, b := range bots {
		if _, ok := matchingBots[b.BotId]; ok {
			alert := alertMap[b.BotId]
			if g.decider.ShouldPowercycleBot(b) {
				downBots = append(downBots, DownBot{
					BotID:      b.BotId,
					Dimensions: b.Dimensions,
					Status:     STATUS_HOST_MISSING,
					Since:      alert.StartsAt,
					Silenced:   alert.Silenced,
				})
			} else if g.decider.ShouldPowercycleDevice(b) {
				downBots = append(downBots, DownBot{
					BotID:      b.BotId,
					Dimensions: b.Dimensions,
					Status:     STATUS_DEVICE_MISSING,
					Since:      alert.StartsAt,
					Silenced:   alert.Silenced,
				})
			}
		}
	}

	// Return sorted based on BotID for determinism and organization.
	sort.Slice(downBots, func(i, j int) bool {
		return downBots[i].BotID < downBots[j].BotID
	})
	g.set(downBots)
	sklog.Infof("Done, found %d bots", len(downBots))
}
