package team

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
	"reasonix/internal/state/teamroster"
)

func TestAnAgentAsksForASecretInItsOwnConversationOnly(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	out, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id("Ada")}, "shop key", "to call the shop API")
	if err != nil || !strings.Contains(out, "$REASONIX_SECRET_SHOP_KEY") {
		t.Fatalf("request = %q, %v", out, err)
	}
	book, _ := h.store.Secrets()
	if len(book.Requests) != 1 || book.Requests[0].Name != "SHOP_KEY" || book.Requests[0].Agent != h.id("Ada") {
		t.Fatalf("requests = %+v", book.Requests)
	}
	g := groupOf(t, h, h.id("Chief"), h.id("Chief"), h.id("Ada"))
	if _, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id("Ada"), Group: g}, "x", ""); !errors.Is(err, ErrPrivateOnly) {
		t.Fatalf("a request from a group seat = %v, want ErrPrivateOnly", err)
	}
}

// The user saves a secret: the request closes, the value is masked wherever
// this process writes for a model, the asking agent is told how to use it and
// never what it is, and only the agents it was given to see it in a shell.
func TestASavedSecretReachesOnlyTheAgentsItWasGivenTo(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	WatchSecrets(h.store)
	ctx := context.Background()
	if _, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id("Ada")}, "SHOP_KEY", "the shop"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UserSaveSecret(ctx, "SHOP_KEY", "sk-shop-0123456789", "the shop", []string{h.id("Ada")}, h.id("Ada")); err != nil {
		t.Fatal(err)
	}
	book, _ := h.store.Secrets()
	if len(book.Requests) != 0 || len(book.Secrets) != 1 {
		t.Fatalf("book = %+v", book)
	}
	if got := secrets.Masker().String("key sk-shop-0123456789"); got != "key [secret:SHOP_KEY]" {
		t.Fatalf("the saved value is not masked: %q", got)
	}
	told := h.panes.pane(SeatRef{Agent: h.id("Ada")}).deliveries()
	if len(told) != 1 || told[0].Kind != DeliverSecret || !strings.Contains(told[0].Text, "$REASONIX_SECRET_SHOP_KEY") || strings.Contains(told[0].Text, "sk-shop") {
		t.Fatalf("Ada was told %+v", told)
	}
	if env := h.svc.SecretEnv(h.id("Ada")); !slices.Contains(env, "REASONIX_SECRET_SHOP_KEY=sk-shop-0123456789") {
		t.Fatalf("Ada's env = %v", env)
	}
	if env := h.svc.SecretEnv(h.id("Ben")); len(env) != 0 {
		t.Fatalf("Ben, who was not given it, has %v", env)
	}
}

// Deleting a secret stops it reaching commands; the copies already in
// conversations stay masked until the person forgets the retired value, and
// a value replaced under the same name is retired the same way.
func TestADeletedOrReplacedValueIsRetiredNotUnmasked(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	WatchSecrets(h.store)
	ctx := context.Background()
	if err := h.svc.UserSaveSecret(ctx, "ROTATED", "sk-rotate-old-0001", "", []string{h.id("Ada")}, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UserSaveSecret(ctx, "ROTATED", "sk-rotate-new-0002", "", []string{h.id("Ada")}, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UserDeleteSecret(ctx, "ROTATED"); err != nil {
		t.Fatal(err)
	}
	if env := h.svc.SecretEnv(h.id("Ada")); len(env) != 0 {
		t.Fatalf("after delete Ada has %v", env)
	}
	if got := secrets.Masker().String("sk-rotate-old-0001 sk-rotate-new-0002"); got != "[secret:ROTATED] [secret:ROTATED]" {
		t.Fatalf("a retired value was unmasked: %q", got)
	}
	book, _ := h.store.Secrets()
	if len(book.Retired) != 2 {
		t.Fatalf("retired = %+v", book.Retired)
	}
	if err := h.svc.UserForgetRetired(ctx); err != nil {
		t.Fatal(err)
	}
	if book, _ := h.store.Secrets(); len(book.Retired) != 0 {
		t.Fatalf("after forgetting, retired = %+v", book.Retired)
	}
}

// Saving closes every request for the name, and every asker hears whether it
// was given the secret: none is left waiting on a card that already closed.
func TestEveryAgentThatAskedHearsTheAnswer(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	ctx := context.Background()
	for _, who := range []string{"Ada", "Ben"} {
		if _, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id(who)}, "SHOP_KEY", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.svc.UserSaveSecret(ctx, "SHOP_KEY", "sk-asked-0123456789", "", []string{h.id("Ada")}, h.id("Ada")); err != nil {
		t.Fatal(err)
	}
	ada := h.panes.pane(SeatRef{Agent: h.id("Ada")}).deliveries()
	ben := h.panes.pane(SeatRef{Agent: h.id("Ben")}).deliveries()
	if len(ada) != 1 || !strings.Contains(ada[0].Text, `status="saved"`) {
		t.Fatalf("Ada was told %+v", ada)
	}
	if len(ben) != 1 || !strings.Contains(ben[0].Text, `status="withheld"`) || strings.Contains(ben[0].Text, "sk-asked") {
		t.Fatalf("Ben was told %+v", ben)
	}
}

// A request left by an agent since deleted closes with the save, and its
// missing seat does not turn a committed save into an error.
func TestADeletedAgentsRequestClosesQuietly(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	if err := h.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		b.Requests = append(b.Requests, teamroster.SecretRequest{Name: "OLD_KEY", Agent: "a-gone"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UserSaveSecret(ctx, "OLD_KEY", "sk-old-key-0123456789", "", []string{h.id("Ada")}, ""); err != nil {
		t.Fatalf("save = %v", err)
	}
	if book, _ := h.store.Secrets(); len(book.Requests) != 0 || len(book.Secrets) != 1 {
		t.Fatalf("book = %+v", book)
	}
	if told := h.panes.pane(SeatRef{Agent: "a-gone"}).deliveries(); len(told) != 0 {
		t.Fatalf("a deleted agent's seat was opened and told %+v", told)
	}
}

func TestAnEmptyListOfAgentsIsRefusedNotReadAsEveryone(t *testing.T) {
	h := newHarness(t, "Chief")
	err := h.svc.UserSaveSecret(context.Background(), "NOBODY", "sk-nobody-0123456789", "", []string{}, "")
	var fe *teamroster.FieldError
	if !errors.As(err, &fe) || fe.Field != "agents" || !errors.Is(err, teamroster.ErrEmpty) {
		t.Fatalf("empty agents = %v", err)
	}
}

// What an agent writes on its request is bounded in the unit the store checks,
// so the card that carries it back can always be saved; and each agent's open
// requests are bounded, so one agent asking for many evicts only its own.
func TestARequestIsBoundedWhereItIsMade(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	ctx := context.Background()
	if _, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id("Ben")}, "BENS", "Ben's own"); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("क्षि", 120)
	if _, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id("Ada")}, "LONG", long); err != nil {
		t.Fatal(err)
	}
	book, _ := h.store.Secrets()
	clipped := book.Requests[1].Purpose
	if err := h.svc.UserSaveSecret(ctx, "LONG", "sk-long-0123456789", clipped, []string{h.id("Ada")}, h.id("Ada")); err != nil {
		t.Fatalf("the clipped purpose a card carries back was refused: %v", err)
	}
	for i := range teamroster.MaxSecretRequests + 5 {
		if _, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id("Ada")}, fmt.Sprintf("K%d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	book, _ = h.store.Secrets()
	if n := countRequests(book.Requests, h.id("Ada")); n != teamroster.MaxSecretRequests {
		t.Fatalf("Ada has %d open requests", n)
	}
	if !slices.ContainsFunc(book.Requests, func(q teamroster.SecretRequest) bool { return q.Agent == h.id("Ben") }) {
		t.Fatal("Ada's requests evicted Ben's")
	}
	if err := h.svc.UserSaveSecret(ctx, "WHY", "sk-why-0123456789", strings.Repeat("why ", 400), []string{h.id("Ada")}, ""); err == nil {
		t.Fatal("an unbounded purpose was saved")
	}
}

func TestADeclinedRequestTellsTheAgent(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	if _, err := h.svc.RequestSecret(ctx, Caller{Agent: h.id("Ada")}, "SHOP_KEY", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UserCancelSecret(ctx, "SHOP_KEY", h.id("Ada")); err != nil {
		t.Fatal(err)
	}
	book, _ := h.store.Secrets()
	told := h.panes.pane(SeatRef{Agent: h.id("Ada")}).deliveries()
	if len(book.Requests) != 0 || len(told) != 1 || !strings.Contains(told[0].Text, `status="declined"`) {
		t.Fatalf("book %+v, told %+v", book, told)
	}
}

// A value another process saved is masked here on the next snapshot: masking
// reads the store, it does not wait to be told.
func TestAValueSavedPastThisServiceIsMaskedAtOnce(t *testing.T) {
	h := newHarness(t, "Chief")
	WatchSecrets(h.store)
	if err := h.store.EditSecrets(context.Background(), func(b *teamroster.SecretBook) error {
		b.Secrets = append(b.Secrets, teamroster.Secret{Name: "ELSEWHERE", Value: "tok-elsewhere-9876543210"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := secrets.Masker().String("tok-elsewhere-9876543210"); got != "[secret:ELSEWHERE]" {
		t.Fatalf("a stored secret was not masked: %q", got)
	}
}
