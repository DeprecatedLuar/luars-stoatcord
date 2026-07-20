package main

// compositeHealthChecker composes the Discord and Stoat adapters' own
// connection health into one engine.HealthChecker, so engine stays ignorant
// of how many gateways exist (implementation-plan.md's stated purpose for
// this composition). Takes plain closures rather than *discordgo.Session/
// *stoat.Gateway directly so this file needs no platform-specific import --
// discordgo stays confined to internal/discord (see permission.go's
// package doc comment).
type compositeHealthChecker struct {
	discordReady func() bool
	stoatHealthy func() bool
}

func (c *compositeHealthChecker) Check() (ok bool, degraded []string) {
	if !c.discordReady() {
		degraded = append(degraded, "discord")
	}
	if !c.stoatHealthy() {
		degraded = append(degraded, "stoat")
	}
	return len(degraded) == 0, degraded
}
