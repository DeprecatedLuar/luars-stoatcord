package main

import (
	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/stoat"
)

// compositeHealthChecker composes the Discord and Stoat adapters' own
// connection health into one engine.HealthChecker, so engine stays ignorant
// of how many gateways exist (implementation-plan.md's stated purpose for
// this composition).
type compositeHealthChecker struct {
	discordSession *discordgo.Session
	stoatGateway   *stoat.Gateway
}

func (c *compositeHealthChecker) Check() (ok bool, degraded []string) {
	if !c.discordSession.DataReady {
		degraded = append(degraded, "discord")
	}
	if !c.stoatGateway.Healthy() {
		degraded = append(degraded, "stoat")
	}
	return len(degraded) == 0, degraded
}
