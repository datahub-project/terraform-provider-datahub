// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Waiting out the assertion-to-monitor link.
//
// Each upsertDataset*AssertionMonitor mutation writes an assertion and a
// monitor in one request, and both entities are committed when it returns. The
// link between them is not. The monitor points at the assertion through an
// Evaluates relationship, and everything that goes from the assertion to its
// monitor -- the assertion.monitor GraphQL field, and the mutation itself on
// update -- walks that relationship in the graph index. For writes that do not
// come from the DataHub UI the graph index is updated asynchronously, after the
// change has passed through DataHub's change log, so the link can trail the
// commit by seconds normally and by minutes on a busy instance. The monitor's
// id is a random UUID and the assertion carries no pointer to it, so there is
// no strongly consistent way to find it in the meantime.
//
// This file holds the bounded, backed-off wait that bridges that gap on create
// and on import (to learn the monitor URN) and on update (where the server
// refuses to write until it can resolve the monitor itself).

package datahub

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// monitorLinkBackoff is a bounded exponential backoff: the first retry waits
// initial, each later one doubles up to max, and no retry starts once budget
// has elapsed since the first attempt.
type monitorLinkBackoff struct {
	initial time.Duration
	max     time.Duration
	budget  time.Duration
}

// defaultMonitorLinkBackoff is how long the provider waits for DataHub to link
// a new assertion to its monitor. The link is indexed asynchronously for every
// caller other than the DataHub UI (see the file comment), and on a busy DataHub
// Cloud tenant that has been observed to take longer than thirty seconds -- the
// budget this replaced. Three minutes covers that with room to spare while
// keeping a genuinely stuck instance from holding an apply open indefinitely;
// starting at half a second keeps the normal case, where the link appears
// within a second or two, as fast as the fixed poll it replaced.
var defaultMonitorLinkBackoff = monitorLinkBackoff{
	initial: 500 * time.Millisecond,
	max:     5 * time.Second,
	budget:  3 * time.Minute,
}

// importMonitorLinkBudget caps the wait for the assertion-to-monitor link
// during terraform import, which is shorter than the create budget because the
// two waits trade off differently.
//
// Import waits at all because it starts with no state: Read keeps a stored
// monitor_urn across a lookup that comes back empty, but an import has nothing
// stored, so one empty lookup would record the assertion with monitor_urn null
// and leave the next destroy depending on the same lagging lookup. Importing an
// assertion created moments earlier is the case that meets the lag, and it is
// common: a create in one configuration followed by an import into another.
//
// A normal refresh does not wait, and must not. It runs on every plan for every
// assertion, its stored monitor_urn already survives an empty lookup, and the
// null it can hold after a create warning is filled by whichever later refresh
// sees the link. Waiting there would only slow terraform plan.
//
// A minute rather than the create budget's three because the failure costs
// less: the import still succeeds and records the assertion, so a slow link
// costs a warning and a later refresh rather than a leaked monitor. And an
// assertion that genuinely has no monitor (its monitor deleted outside
// Terraform, say) makes every import of it spend the whole budget, which should
// stay tolerable for something run by hand. The link normally appears within
// seconds, so a minute still covers a busy tenant comfortably.
const importMonitorLinkBudget = time.Minute

// forImport returns b with its budget capped at importMonitorLinkBudget. A
// test override shorter than that is kept, so tests that shorten the wait
// shorten the import wait too.
func (b monitorLinkBackoff) forImport() monitorLinkBackoff {
	b.budget = min(b.budget, importMonitorLinkBudget)
	return b
}

// monitorLinkBackoffOverride, when set, replaces defaultMonitorLinkBackoff.
// Only tests set it, through SetMonitorLinkBackoffForTesting.
var monitorLinkBackoffOverride atomic.Pointer[monitorLinkBackoff]

func currentMonitorLinkBackoff() monitorLinkBackoff {
	if b := monitorLinkBackoffOverride.Load(); b != nil {
		return *b
	}
	return defaultMonitorLinkBackoff
}

// SetMonitorLinkBackoffForTesting shortens the wait for the assertion-to-monitor
// link so a test can drive it past its budget in milliseconds rather than
// minutes. It returns a function that restores the default; call it from
// t.Cleanup. Not for use outside tests.
func SetMonitorLinkBackoffForTesting(initial, maxInterval, budget time.Duration) (restore func()) {
	monitorLinkBackoffOverride.Store(&monitorLinkBackoff{initial: initial, max: maxInterval, budget: budget})
	return func() { monitorLinkBackoffOverride.Store(nil) }
}

// errMonitorLinkBudgetSpent reports that the backoff ran out of budget while
// the condition it was waiting for still had not occurred.
var errMonitorLinkBudgetSpent = errors.New("gave up waiting")

// poll runs attempt until it succeeds, fails permanently, the budget is spent,
// or ctx is done. attempt returns retry=true to ask for another try, with err
// describing why (it may be nil); retry=false ends the loop with err as the
// result. When the budget is spent the result wraps both
// errMonitorLinkBudgetSpent and the last retryable error.
func (b monitorLinkBackoff) poll(ctx context.Context, attempt func() (retry bool, err error)) error {
	deadline := time.Now().Add(b.budget)
	delay := b.initial
	for {
		retry, err := attempt()
		if !retry {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if err != nil {
				return fmt.Errorf("%w after %s: %w", errMonitorLinkBudgetSpent, b.budget, err)
			}
			return fmt.Errorf("%w after %s", errMonitorLinkBudgetSpent, b.budget)
		}
		wait := min(delay, remaining)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			if err != nil {
				return fmt.Errorf("%w (last attempt: %w)", ctx.Err(), err)
			}
			return fmt.Errorf("waiting for the assertion monitor link: %w", ctx.Err())
		case <-timer.C:
		}
		delay = min(delay*2, b.max)
	}
}

// MonitorPendingError is returned by the typed assertion upserts when the
// assertion and its monitor were created but DataHub had not linked them by the
// end of the wait, so the monitor URN is unknown. The assertion URN is valid and
// is returned alongside this error: callers must record it rather than treat
// the create as failed, or the assertion and monitor are left behind untracked.
type MonitorPendingError struct {
	AssertionURN string
	Cause        error
}

func (e *MonitorPendingError) Error() string {
	return fmt.Sprintf("assertion %q was created, but DataHub has not yet linked it to its monitor: %v", e.AssertionURN, e.Cause)
}

func (e *MonitorPendingError) Unwrap() error { return e.Cause }

// monitorNotLinkedPattern matches the error an upsertDataset*AssertionMonitor
// update returns when the server cannot resolve the assertion's monitor through
// the graph index. The server checks this before writing anything, so the
// update is safe to resend.
var monitorNotLinkedPattern = regexp.MustCompile(`Monitor for assertion urn:li:assertion:\S+ does not exist`)

// isMonitorNotLinkedError reports whether msg is the server's "monitor does not
// exist" refusal of an assertion update. It deliberately matches that one
// message and nothing broader: every other failure is either permanent or not
// known to be free of side effects, and retrying it would only delay the error.
//
// Releases of DataHub that replace unexpected server errors with a generic
// "An unknown error occurred" message hide this text; the update path's wait
// for the link before sending (see sendAssertionMonitorUpdate) is what covers
// those.
func isMonitorNotLinkedError(msg string) bool {
	return monitorNotLinkedPattern.MatchString(msg)
}

// IsMonitorLinkWaitExhausted reports whether err is a wait for the
// assertion-to-monitor link that ran out of budget with the link still not
// visible, as opposed to a lookup that failed or a cancelled context.
func IsMonitorLinkWaitExhausted(err error) bool {
	return errors.Is(err, errMonitorLinkBudgetSpent)
}

// AwaitAssertionMonitorForImport returns the URN of an assertion's monitor for
// terraform import, waiting up to importMonitorLinkBudget for DataHub to link
// the two (see that constant for why import waits and refresh does not). When
// the link is still not visible at the end of the wait it returns an error for
// which IsMonitorLinkWaitExhausted is true; a failed lookup or an ended ctx
// returns that error instead.
func (c *Client) AwaitAssertionMonitorForImport(ctx context.Context, assertionURN string) (string, error) {
	return c.waitForAssertionMonitor(ctx, currentMonitorLinkBackoff().forImport(), assertionURN)
}

// waitForAssertionMonitor polls the assertion.monitor lookup with backoff b
// until it returns the monitor's URN. It returns an error wrapping
// errMonitorLinkBudgetSpent when the link is still not visible at the end of
// the budget, ctx's error when ctx ends first, and the lookup's own error when
// a lookup fails.
func (c *Client) waitForAssertionMonitor(ctx context.Context, b monitorLinkBackoff, assertionURN string) (string, error) {
	var monitorURN string
	start := time.Now()
	lookups := 0
	err := b.poll(ctx, func() (bool, error) {
		lookups++
		urn, err := c.GetAssertionMonitorURN(ctx, assertionURN)
		if err != nil {
			return false, fmt.Errorf("looking up the monitor for assertion %q: %w", assertionURN, err)
		}
		if urn == "" {
			return true, nil
		}
		monitorURN = urn
		return false, nil
	})
	// Logged either way: how long the link takes to appear is the one number
	// that says whether the budget is still right.
	tflog.Debug(ctx, "DataHub assertion monitor link wait finished", map[string]any{
		"assertion_urn": assertionURN,
		"monitor_urn":   monitorURN,
		"lookups":       lookups,
		"elapsed_ms":    time.Since(start).Milliseconds(),
		"budget_ms":     b.budget.Milliseconds(),
	})
	if err != nil {
		return "", err
	}
	return monitorURN, nil
}

// upsertAssertionMonitor sends one of the five upsertDataset*AssertionMonitor
// mutations and returns the assertion URN and, on create, the monitor URN.
//
// field is the mutation's name, which is also the key of its result. kind names
// the assertion type in error messages. existingURN is the assertion URN from
// state, empty on create.
//
// On create the mutation is sent once. Its success means both entities exist,
// so a monitor URN that cannot be learned afterwards is reported as a
// *MonitorPendingError alongside the assertion URN, never as a bare failure.
//
// On update the mutation is resent while it fails with the server's "monitor
// does not exist" refusal, within the monitor link budget; see
// sendAssertionMonitorUpdate. The monitor URN is returned empty because the
// caller already holds it.
func (c *Client) upsertAssertionMonitor(ctx context.Context, body map[string]any, field, kind, existingURN string) (string, string, error) {
	send := func() (string, error) {
		var raw struct {
			Data map[string]*struct {
				URN string `json:"urn"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := c.doGraphQL(ctx, body, &raw); err != nil {
			return "", err
		}
		if len(raw.Errors) > 0 {
			msg := raw.Errors[0].Message
			if isAssertionCloudOnlyError(msg) {
				return "", ErrAssertionCloudOnly
			}
			return "", &graphQLMessageError{msg: msg}
		}
		result := raw.Data[field]
		if result == nil || result.URN == "" {
			return "", fmt.Errorf("DataHub API error: %s returned no assertion URN", field)
		}
		return result.URN, nil
	}

	if existingURN != "" {
		urn, err := c.sendAssertionMonitorUpdate(ctx, existingURN, send)
		return urn, "", err
	}

	assertionURN, err := send()
	if err != nil {
		return "", "", err
	}
	monitorURN, err := c.waitForAssertionMonitor(ctx, currentMonitorLinkBackoff(), assertionURN)
	if err != nil {
		return assertionURN, "", &MonitorPendingError{
			AssertionURN: assertionURN,
			Cause:        fmt.Errorf("%s assertion monitor link not visible: %w", kind, err),
		}
	}
	return assertionURN, monitorURN, nil
}

// sendAssertionMonitorUpdate sends an assertion update, first waiting for the
// assertion's monitor link to be visible and then resending while the server
// refuses with isMonitorNotLinkedError, all within one monitor link budget.
//
// The server resolves the monitor through the same graph relationship that the
// assertion.monitor lookup reads, so waiting for the lookup first avoids the
// refusal without depending on its wording. The resend covers the remaining
// window in which the lookup and the server's own read disagree. A failed or
// empty lookup does not stop the update: the server is the authority on whether
// it can proceed.
func (c *Client) sendAssertionMonitorUpdate(ctx context.Context, assertionURN string, send func() (string, error)) (string, error) {
	b := currentMonitorLinkBackoff()
	start := time.Now()
	if _, err := c.waitForAssertionMonitor(ctx, b, assertionURN); err != nil && ctx.Err() != nil {
		return "", fmt.Errorf("updating assertion %q: %w", assertionURN, ctx.Err())
	}
	b.budget -= time.Since(start)

	var urn string
	err := b.poll(ctx, func() (bool, error) {
		u, err := send()
		if err == nil {
			urn = u
			return false, nil
		}
		var gqlErr *graphQLMessageError
		if errors.As(err, &gqlErr) && isMonitorNotLinkedError(gqlErr.msg) {
			return true, err
		}
		return false, err
	})
	if errors.Is(err, errMonitorLinkBudgetSpent) {
		return "", fmt.Errorf("%w. DataHub has the assertion's monitor but has not yet indexed the link "+
			"to it, which it needs before it can update the assertion; retry the apply in a few minutes", err)
	}
	if err != nil {
		return "", err
	}
	return urn, nil
}

// graphQLMessageError is a GraphQL error message returned by the server,
// rendered the way the assertion functions have always reported one. It is a
// type rather than a string so the update path can inspect the server's text
// without parsing it back out of a formatted error.
type graphQLMessageError struct{ msg string }

func (e *graphQLMessageError) Error() string { return "DataHub API error: " + e.msg }
