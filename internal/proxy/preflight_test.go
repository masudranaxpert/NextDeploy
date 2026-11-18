package proxy

import "testing"

func testCaddyCaps() Capabilities {
	return Capabilities{
		AutoTLS:                 true,
		InternalTLS:             true,
		OnDemandTLS:             true,
		DockerUpstreamDiscovery: true,
		PHPFastCGI:              true,
		BasicAuthBcrypt:         true,
		RegexRouting:            true,
		RequireTargetFile:       true,
		H2IdleTimeout:           true,
		WriteTimeout:            true,
		ReadHeaderTimeout:       true,
		ReadBodyTimeout:         true,
		GracePeriod:             true,
		MaxHeaderSizeHTTP1:      true,
		TrustedStrict:           true,
		LongStreamsOnReload:     true,
		WordPressPageCache:      true,
		AdminAPI:                true,
		Experimental:            false,
	}
}

func testAngieCaps() Capabilities {
	c := testCaddyCaps()
	c.OnDemandTLS = false
	return c
}

func testFerronCaps() Capabilities {
	return Capabilities{
		AutoTLS:                 true,
		InternalTLS:             true,
		OnDemandTLS:             true,
		DockerUpstreamDiscovery: false,
		PHPFastCGI:              true,
		BasicAuthBcrypt:         true,
		RegexRouting:            true,
		RequireTargetFile:       false,
		H2IdleTimeout:           false,
		WriteTimeout:            false,
		ReadHeaderTimeout:       false,
		ReadBodyTimeout:         false,
		GracePeriod:             false,
		MaxHeaderSizeHTTP1:      false,
		TrustedStrict:           false,
		LongStreamsOnReload:     true,
		AdminAPI:                true,
		Experimental:            true,
	}
}

func hasIssue(issues []PreflightIssue, capKey string) (PreflightIssue, bool) {
	for _, iss := range issues {
		if iss.Capability == capKey {
			return iss, true
		}
	}
	return PreflightIssue{}, false
}

func TestEvaluateCapabilities_CaddyAndAngieCompatible(t *testing.T) {
	st := fullSampleState()

	caddyDiff := EvaluateCapabilities(&st, testCaddyCaps())
	if !caddyDiff.CanSwitch || caddyDiff.RequiresExperimentalAck || len(caddyDiff.HardBlocks) != 0 || len(caddyDiff.SoftWarnings) != 0 {
		t.Fatalf("expected clean Caddy preflight, got %+v", caddyDiff)
	}

	angieDiff := EvaluateCapabilities(&st, testAngieCaps())
	if !angieDiff.CanSwitch || angieDiff.RequiresExperimentalAck || len(angieDiff.HardBlocks) != 0 || len(angieDiff.SoftWarnings) != 0 {
		t.Fatalf("expected clean Angie preflight for standard IR, got %+v", angieDiff)
	}

	// Explicit on_demand_tls site blocks Angie.
	stOnDemand := fullSampleState()
	stOnDemand.Sites[0].OnDemandTLS = true
	angieBlock := EvaluateCapabilities(&stOnDemand, testAngieCaps())
	if angieBlock.CanSwitch {
		t.Fatalf("expected Angie to block on explicit OnDemandTLS site")
	}
	if iss, ok := hasIssue(angieBlock.HardBlocks, "on_demand_tls"); !ok || len(iss.AffectedSites) != 1 {
		t.Fatalf("expected on_demand_tls hard block with 1 affected site, got %+v", angieBlock.HardBlocks)
	}
}

func TestEvaluateCapabilities_FerronBlocksAndWarnings(t *testing.T) {
	st := fullSampleState()
	// Add an enabled RequireTargetFile rule on the PHP site and a disabled one on another site.
	st.Sites[1].PHP.Routing = append(st.Sites[1].PHP.Routing, RoutingRule{
		ID:                "req-php",
		Kind:              "rewrite",
		MatchType:         "regex",
		Pattern:           `^/([^.]+)$`,
		Target:            "/$1.php",
		RequireTargetFile: true,
		Enabled:           true,
	})
	st.Sites = append(st.Sites, Site{
		ID:    "php:disabled-rule",
		Hosts: []string{"disabled.example.com"},
		TLS:   TLSAuto,
		Kind:  SiteKindPHP,
		PHP: &PHPSpec{
			DocRoot: "/srv/disabled",
			Socket:  "/run/disabled.sock",
			Routing: []RoutingRule{
				{ID: "off", Kind: "rewrite", MatchType: "exact", Pattern: "/x", Target: "/x.php", RequireTargetFile: true, Enabled: false},
			},
		},
	})

	diff := EvaluateCapabilities(&st, testFerronCaps())
	if diff.CanSwitch {
		t.Fatalf("expected Ferron preflight to block, got CanSwitch=true: %+v", diff)
	}
	if !diff.RequiresExperimentalAck {
		t.Fatalf("expected RequiresExperimentalAck=true for Ferron")
	}

	// Verify HardBlocks: require_target_file (with blog only, not disabled-rule) & trusted_strict.
	rtf, ok := hasIssue(diff.HardBlocks, "require_target_file")
	if !ok {
		t.Fatalf("missing require_target_file hard block: %+v", diff.HardBlocks)
	}
	if len(rtf.AffectedSites) != 1 || rtf.AffectedSites[0].SiteID != "php:blog" || rtf.AffectedSites[0].Label != "blog.example.com" {
		t.Fatalf("unexpected AffectedSites for require_target_file: %+v", rtf.AffectedSites)
	}
	if _, ok := hasIssue(diff.HardBlocks, "trusted_strict"); !ok {
		t.Fatalf("expected trusted_strict hard block when Global.TrustedStrict=true: %+v", diff.HardBlocks)
	}

	// Verify SoftWarnings cover experimental + all configured Ferron timeout/header gaps.
	for _, wantWarn := range []string{
		"experimental",
		"h2_idle_timeout",
		"write_timeout",
		"read_header_timeout",
		"read_body_timeout",
		"grace_period",
		"max_header_size_http1",
	} {
		if _, ok := hasIssue(diff.SoftWarnings, wantWarn); !ok {
			t.Errorf("missing expected Ferron soft warning %q in %+v", wantWarn, diff.SoftWarnings)
		}
	}
}

func TestEvaluateCapabilities_FerronCloudflareNonStrictWarnsWithoutBlock(t *testing.T) {
	st := State{
		Global: GlobalOptions{
			Profile:          "default",
			RealIP:           "cloudflare",
			DirectCloudflare: true,
		},
		Sites: []Site{
			{
				ID:           "app:1:dom:1",
				Hosts:        []string{"cf.example.com"},
				TLS:          TLSAuto,
				TrustedProxy: true,
				Kind:         SiteKindAppProxy,
				Upstream:     &UpstreamRef{Name: "nd_1_web_3000", Port: 3000},
			},
		},
	}

	diff := EvaluateCapabilities(&st, testFerronCaps())
	if !diff.CanSwitch {
		t.Fatalf("expected switch to be allowed with warnings when TrustedStrict=false, got hard blocks: %+v", diff.HardBlocks)
	}
	if !diff.RequiresExperimentalAck {
		t.Fatal("expected RequiresExperimentalAck=true")
	}
	ts, ok := hasIssue(diff.SoftWarnings, "trusted_strict")
	if !ok {
		t.Fatalf("expected trusted_strict soft warning for cloudflare real_ip, got %+v", diff.SoftWarnings)
	}
	if len(ts.AffectedSites) != 1 || ts.AffectedSites[0].SiteID != "app:1:dom:1" {
		t.Fatalf("expected cf site in trusted_strict warning, got %+v", ts.AffectedSites)
	}
	// Default profile has empty ReadHeader/ReadBodyIdle/GracePeriod/MaxHeaderSize -> no unconfigured field warnings.
	for _, unexpected := range []string{"read_header_timeout", "read_body_timeout", "grace_period", "max_header_size_http1"} {
		if _, ok := hasIssue(diff.SoftWarnings, unexpected); ok {
			t.Errorf("did not expect warning %q when option is unset", unexpected)
		}
	}
}

func TestEvaluateCapabilities_MinimalMissingCapabilitiesAndNilState(t *testing.T) {
	// Nil state must never panic.
	nilDiff := EvaluateCapabilities(nil, testCaddyCaps())
	if !nilDiff.CanSwitch || len(nilDiff.HardBlocks) != 0 {
		t.Fatalf("unexpected nil state diff: %+v", nilDiff)
	}

	// Empty capabilities block every required per-site feature and warn on LongStreamsOnReload.
	st := fullSampleState()
	bare := Capabilities{}
	diff := EvaluateCapabilities(&st, bare)
	if diff.CanSwitch {
		t.Fatal("expected CanSwitch=false for empty Capabilities")
	}
	for _, wantBlock := range []string{"auto_tls", "internal_tls", "php_fastcgi", "basic_auth_bcrypt", "regex_routing"} {
		if _, ok := hasIssue(diff.HardBlocks, wantBlock); !ok {
			t.Errorf("expected hard block %q in %+v", wantBlock, diff.HardBlocks)
		}
	}
	if ls, ok := hasIssue(diff.SoftWarnings, "long_streams_on_reload"); !ok || len(ls.AffectedSites) == 0 {
		t.Errorf("expected long_streams_on_reload soft warning with affected sites, got %+v", diff.SoftWarnings)
	}
}
