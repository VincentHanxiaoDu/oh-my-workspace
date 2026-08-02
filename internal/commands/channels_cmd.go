// Command `omw channels` — connecting Teams and email once, and seeing what continuous ingestion
// has done with them (PRD §3.1, §3.2, §4.2, §4.3; Issue #6).
//
// THERE IS NO `ingest` SUBCOMMAND AND THERE WILL NOT BE ONE. Criterion 4: ingestion is a property
// of the daemon running, not of a command being typed, so the only thing that ingests is
// `internal/channels`'s registered background work. Every subcommand here reads; none of them
// ingests, and none of them starts the daemon (criterion 7).
//
// This file is the ONLY file this Issue adds to package commands, and it references nothing else in
// the package: its store resolution is its own, spelled out again rather than borrowed from the
// daemon command's helper, so that neither Issue's file appears in the other's diff.
package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/channels"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/daemon"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/store"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

func init() {
	cli.Register(&cli.Command{
		Name:    "channels",
		Summary: "connect Teams and email, and see what ingestion has made of them",
		Run:     runChannels,
	})
}

const channelsUsage = `usage: omw channels <connect|list|status|disconnect> [arguments]

  connect <kind> --account <who> --credential-file <path> [--id <name>]
                     connect a channel. The kinds are: email, teams — both built in.
                     The credential is YOURS to supply: nothing here signs you in.
  list               every connected channel, its kind, and what ingestion last did.
  status <id>        one channel, including one that is not connected.
  disconnect <id>    stop ingesting a channel.

Ingestion happens because the daemon is running, not because you typed something. There is no
ingest command and no refresh command (PRD §3.1). While the daemon is stopped nothing is
ingested, and every command here says so rather than showing you a time that stopped being
current when the daemon did.

The store comes from $` + store.PathEnv + `, else this device's registered store, else a
per-user data directory.
`

func runChannels(env cli.Env) int {
	if len(env.Args) == 0 {
		fmt.Fprint(env.Stderr, channelsUsage)
		return cli.ExitUsage
	}
	sub, rest := env.Args[0], env.Args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprint(env.Stdout, channelsUsage)
		return cli.Success
	case "connect":
		return channelsConnect(env, rest)
	case "list":
		return channelsList(env, rest)
	case "status":
		return channelsStatus(env, rest)
	case "disconnect":
		return channelsDisconnect(env, rest)
	default:
		fmt.Fprintf(env.Stderr, "omw channels: unknown subcommand %q\n", sub)
		fmt.Fprint(env.Stderr, channelsUsage)
		return cli.ExitUsage
	}
}

// channelsStore resolves and opens this device's store WITHOUT creating one, and WITHOUT starting
// anything.
func channelsStore(env cli.Env) (*store.Store, string, int) {
	path, err := store.Resolve(env.Getenv)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw channels: %v\n", err)
		if errors.Is(err, store.ErrPathUndetermined) {
			// UNDETERMINED, NOT MISSING, AND ITS OWN CODE. Not knowing where the store lives is not
			// knowing there is not one.
			return nil, "", cli.ExitUndetermined
		}
		return nil, "", cli.ExitFailure
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw channels: %v\n", err)
		if errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(env.Stderr, "  Nothing creates a store on your behalf. Run 'omw store create'.\n")
			return nil, "", cli.ExitFailure
		}
		return nil, "", cli.ExitUndetermined
	}
	return s, path, cli.Success
}

// ingestionRunning is whether ingestion is happening right now, in three values.
//
// IT IS THE DAEMON'S RUNNING STATE AND NOTHING ELSE, because that is exactly what ingestion is
// (criterion 4). daemon.Inspect reads the lock and the run record; it starts nothing (criterion 7).
func ingestionRunning(path string) (tri.Value, string) {
	rep := daemon.Inspect(path)
	switch rep.Running {
	case tri.Yes:
		return tri.Yes, "the daemon is running, so your channels are being ingested continuously"
	case tri.No:
		return tri.No, "the daemon is NOT running, so nothing is being ingested and the ingestion " +
			"facts below are not current"
	default:
		return tri.Undetermined, "whether the daemon is running " + tri.Undetermined.String() +
			", so whether ingestion is happening — and whether the facts below are current — " +
			tri.Undetermined.String()
	}
}

// sayIngestionStanding prints the standing of every ingestion fact that follows it.
//
// CRITERION 6, AND IT IS PRINTED BEFORE THE FACTS RATHER THAN AFTER. A person who stops reading
// after the first screen has still been told. The per-channel renderings repeat it inline, because
// a banner three lines above a timestamp is not what a person copies into a message to somebody.
func sayIngestionStanding(w io.Writer, running tri.Value, why string) {
	fmt.Fprintf(w, "ingestion: %s\n", running.Render("running", "NOT RUNNING"))
	fmt.Fprintf(w, "  %s\n", why)
	if running != tri.Yes {
		fmt.Fprintf(w, "  Nothing here starts the daemon for you (PRD §4.2). Run 'omw daemon start'.\n")
	}
	fmt.Fprintln(w)
}

// exitFor turns the standing into an exit code.
//
// `COULD NOT DETERMINE` AND `DETERMINED TO BE NOTHING` DO NOT SHARE A CODE. A list that is empty is
// an answer and exits zero; a list whose currency could not be established exits ExitUndetermined.
func exitFor(running tri.Value) int {
	if running == tri.Undetermined {
		return cli.ExitUndetermined
	}
	return cli.Success
}

// ---------------------------------------------------------------------------------------------
// connect
// ---------------------------------------------------------------------------------------------

// credentialFile is the sign-in artifact the PERSON supplies.
//
// CRITERION 13, AND IT IS THE WHOLE REASON THIS IS A FILE AND NOT A FLOW. Nothing in this product
// obtains a credential on anybody's behalf: there is no browser opened here, no device-code poll,
// no token endpoint, no keychain read. A person signs in wherever they sign in, and hands the
// result over by naming a file. Connecting is their explicit act and it is the only thing that
// authorises this build to reach that service at all (criterion 11).
type credentialFile struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func channelsConnect(env cli.Env, args []string) int {
	var kind, account, credPath, id string
	var positional []string
	// The three long options, each accepting `--flag value` and `--flag=value`. Written out rather
	// than routed through the flag package because a command that must print its own refusals to
	// its own env.Stderr cannot use a package that prints to os.Stderr and calls os.Exit.
	targets := map[string]*string{"--account": &account, "--credential-file": &credPath, "--id": &id}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, inline, hasInline := strings.Cut(a, "=")
		dest, known := targets[name]
		switch {
		case known && hasInline:
			*dest = inline
		case known && i+1 < len(args):
			i++
			*dest = args[i]
		case known:
			fmt.Fprintf(env.Stderr, "omw channels connect: %s needs a value\n", name)
			return cli.ExitUsage
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(env.Stderr, "omw channels connect: unknown option %q\n", a)
			return cli.ExitUsage
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) != 1 {
		fmt.Fprintf(env.Stderr, "omw channels connect: name exactly one kind to connect (%s)\n",
			strings.Join(kindNames(), ", "))
		return cli.ExitUsage
	}
	kind = positional[0]
	if !channels.Kind(kind).Valid() {
		fmt.Fprintf(env.Stderr, "omw channels connect: %v: %q. This build has: %s\n",
			channels.ErrUnknownKind, kind, strings.Join(kindNames(), ", "))
		return cli.ExitUsage
	}
	if strings.TrimSpace(account) == "" {
		fmt.Fprintf(env.Stderr, "omw channels connect: --account is required; a channel is connected as somebody.\n")
		return cli.ExitUsage
	}
	if strings.TrimSpace(credPath) == "" {
		// CRITERION 13 AS A REFUSAL. Not a prompt, not a browser, not a half-connected channel
		// waiting to be finished later.
		fmt.Fprintf(env.Stderr, "omw channels connect: %v\n", channels.ErrNoCredential)
		fmt.Fprintf(env.Stderr, "  Sign in wherever you sign in, and pass the result with --credential-file.\n")
		fmt.Fprintf(env.Stderr, "  The file may be {\"token\":\"…\",\"expires_at\":\"2026-01-01T00:00:00Z\"} or the token alone.\n")
		return cli.ExitFailure
	}

	body, err := os.ReadFile(credPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "omw channels connect: the credential file could not be read: %v\n", err)
		return cli.ExitFailure
	}
	token, expires, cerr := parseCredential(body)
	if cerr != nil {
		fmt.Fprintf(env.Stderr, "omw channels connect: %v\n", cerr)
		return cli.ExitFailure
	}

	if id == "" {
		id = defaultChannelID(kind, account)
	}

	s, path, code := channelsStore(env)
	if code != cli.Success {
		return code
	}
	conn := channels.Connection{
		ID:                  id,
		Kind:                channels.Kind(kind),
		Account:             account,
		ConnectedAt:         time.Now().UTC(),
		Credential:          token,
		CredentialExpiresAt: expires,
	}
	if err := channels.Connect(s, conn); err != nil {
		fmt.Fprintf(env.Stderr, "omw channels connect: %v\n", err)
		return cli.ExitFailure
	}

	fmt.Fprintf(env.Stdout, "connected %s as %s, under the identifier %s\n", conn.Kind, conn.Account, conn.ID)
	// CONNECTING DOES NOT INGEST AND DOES NOT START ANYTHING. Said, so that a person who sees no
	// tickets afterwards knows why rather than concluding nobody has asked them for anything.
	running, why := ingestionRunning(path)
	fmt.Fprintf(env.Stdout, "ingestion: %s — %s\n", running.Render("running", "NOT RUNNING"), why)
	return exitFor(running)
}

// parseCredential reads the person's sign-in artifact. A file that is not JSON is taken as the
// token itself, with NO expiry — which is [channels.HealthUndetermined] and not a healthy channel.
func parseCredential(body []byte) (token string, expires time.Time, err error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", time.Time{}, fmt.Errorf("%w: the credential file is empty", channels.ErrNoCredential)
	}
	var f credentialFile
	if jerr := json.Unmarshal([]byte(trimmed), &f); jerr != nil || f.Token == "" {
		return trimmed, time.Time{}, nil
	}
	if f.ExpiresAt == "" {
		return f.Token, time.Time{}, nil
	}
	at, perr := time.Parse(time.RFC3339, f.ExpiresAt)
	if perr != nil {
		return "", time.Time{}, fmt.Errorf("the credential file's expires_at could not be read: %v", perr)
	}
	return f.Token, at, nil
}

// defaultChannelID is a readable identifier derived from the kind and the account, so that
// connecting two mailboxes does not need the person to invent names.
func defaultChannelID(kind, account string) string {
	var b strings.Builder
	b.WriteString(kind)
	b.WriteString("-")
	for _, r := range strings.ToLower(account) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

func kindNames() []string {
	out := make([]string, 0, len(channels.Kinds()))
	for _, k := range channels.Kinds() {
		out = append(out, string(k))
	}
	return out
}

// ---------------------------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------------------------

func channelsList(env cli.Env, args []string) int {
	if len(args) > 0 {
		fmt.Fprintf(env.Stderr, "omw channels list: takes no arguments\n")
		return cli.ExitUsage
	}
	s, path, code := channelsStore(env)
	if code != cli.Success {
		return code
	}
	running, why := ingestionRunning(path)
	sayIngestionStanding(env.Stdout, running, why)

	conns, err := channels.List(s)
	if err != nil {
		// UNREADABLE IS NOT EMPTY (criterion 2, §4.3). Its own sentence, its own stream, and its
		// own exit code — a list that could not be read must never be scripted as a list of none.
		fmt.Fprintf(env.Stderr, "omw channels list: the connected channels could not be read: %v\n", err)
		return cli.ExitUndetermined
	}
	if len(conns) == 0 {
		// STATED, NOT PRINTED AS BLANK (criterion 2).
		fmt.Fprintf(env.Stdout, "no channels are connected. This is an empty channel set, not a "+
			"failure to read one.\n")
		fmt.Fprintf(env.Stdout, "Connect one with 'omw channels connect email --account <you> "+
			"--credential-file <path>'.\n")
		return exitFor(running)
	}
	fmt.Fprintf(env.Stdout, "%d connected channel(s):\n\n", len(conns))
	at := time.Now().UTC()
	undetermined := false
	for _, c := range conns {
		fmt.Fprint(env.Stdout, c.Render(at, running))
		fmt.Fprintln(env.Stdout)
		if h, _ := c.Health(at); h == channels.HealthUndetermined {
			undetermined = true
		}
		if !c.Last.State.Determined() {
			undetermined = true
		}
	}
	if undetermined {
		return cli.ExitUndetermined
	}
	return exitFor(running)
}

// ---------------------------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------------------------

func channelsStatus(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw channels status: name exactly one channel\n")
		return cli.ExitUsage
	}
	s, path, code := channelsStore(env)
	if code != cli.Success {
		return code
	}
	running, why := ingestionRunning(path)
	sayIngestionStanding(env.Stdout, running, why)

	c, err := channels.Get(s, args[0])
	switch {
	case errors.Is(err, channels.ErrNoSuchChannel):
		// CRITERION 13'S THIRD STATE. Disconnected is a real, determined answer about a channel
		// somebody named — its own sentence, and NOT the sentence an expired credential gets.
		fmt.Fprintf(env.Stdout, "%s\n  connection:                 %s\n", args[0], channels.HealthDisconnected)
		fmt.Fprintf(env.Stdout, "                              nothing is ingested from a channel that is not connected\n")
		return exitFor(running)
	case err != nil:
		fmt.Fprintf(env.Stderr, "omw channels status: %v\n", err)
		return cli.ExitUndetermined
	}
	fmt.Fprint(env.Stdout, c.Render(time.Now().UTC(), running))
	if h, _ := c.Health(time.Now().UTC()); h == channels.HealthUndetermined || !c.Last.State.Determined() {
		return cli.ExitUndetermined
	}
	return exitFor(running)
}

// ---------------------------------------------------------------------------------------------
// disconnect
// ---------------------------------------------------------------------------------------------

func channelsDisconnect(env cli.Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(env.Stderr, "omw channels disconnect: name exactly one channel\n")
		return cli.ExitUsage
	}
	s, _, code := channelsStore(env)
	if code != cli.Success {
		return code
	}
	if err := channels.Disconnect(s, args[0]); err != nil {
		// A PERSON WHO MISTYPED HEARS ABOUT IT, and the exit code says so on its own (criterion 15).
		fmt.Fprintf(env.Stderr, "omw channels disconnect: %v\n", err)
		if errors.Is(err, channels.ErrNoSuchChannel) {
			return cli.ExitFailure
		}
		return cli.ExitUndetermined
	}
	fmt.Fprintf(env.Stdout, "%s is disconnected; nothing further will be ingested from it\n", args[0])
	fmt.Fprintf(env.Stdout, "the tickets it already produced are yours and stay where they are (PRD §5.4)\n")
	return cli.Success
}
