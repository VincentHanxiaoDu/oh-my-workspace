// Package channels is where people reach this person — Teams and email — and the continuous
// ingestion that turns what arrives there into tickets (PRD §2.1, §3.1, §3.2, §4.2, §4.3; Issue #6).
//
// # INGESTION PRODUCES TICKETS, NOT A MIRROR OF THE TRAFFIC
//
// This is the whole point of the capability and it is the sentence every other decision here is
// downstream of. Eleven messages are not eleven tickets. Five emails and a follow-up ping about one
// broken login are ONE ticket, with a WRITTEN title and a WRITTEN summary — never a copied subject
// line and never a message body. A message count and a ticket count are therefore two different
// numbers, and [Result] reports both, per channel, so that a person can see the difference rather
// than take it on trust.
//
// # ACKNOWLEDGEMENTS PRODUCE NOTHING, AND THERE IS NOWHERE TO PUT THEM
//
// PRD §3.2: acknowledgements and small talk are not low-priority tickets, they are not tickets.
// There is no priority, rank, severity, score, bucket, tag or state in this package that such
// traffic maps to, and there is nowhere downstream either: [inbox.Ticket] has no priority field and
// a reflection test on that package keeps it that way. A matter whose every message is an
// acknowledgement produces NO ticket at all — see [Ingest] and [isRequest].
//
// # UNREACHED IS NOT EMPTY (§4.3)
//
// A channel whose ingestion attempt failed and a channel that was reached and had nothing new both
// produce zero tickets, and they are two different facts. [Outcome] holds them apart, and
// [Connection.Render] prints them differently. Collapsing them is how a person reads "nobody has
// asked me for anything" off a rejected credential.
//
// # INGESTION IS A PROPERTY OF THE DAEMON RUNNING (§2.1, §4.2)
//
// There is no ingest command and no refresh command. [RegisterIngestion] hands the work to the
// daemon as background work at [IngestInterval]; while the daemon runs, the store keeps up. While
// it does not, nothing ingests — and every command in this capability says so rather than showing a
// last-ingestion time that stopped being current when the daemon stopped. No command here starts
// the daemon.
//
// # NOTHING REACHES OUT WITHOUT CAUSE (§4.2, §4.4)
//
// [Ingest] constructs an adapter only for a channel a person has explicitly connected. With no
// channel connected it builds none, so there is nothing that could open a connection — asserted by
// TestIngestionWithNoChannelsConnectedTouchesNoAdapter, which counts factory calls rather than
// trusting the reading. This package does not import the hub and has no route to one: ingesting is
// local work on local traffic, and a hub is not part of it. There is no hub-shaped degradation
// here — with no hub configured this capability works in full (§4.4).
//
// # RAW MESSAGE BODIES ARE NOT CARRIED ANYWHERE (§2.3, §3.2)
//
// A [Message] exists for the duration of one ingestion run and is never stored. The ticket that
// comes out of it holds written text about the matter and no verbatim body, and the connection
// record holds no message at all. The credential a person supplies is stored — the store is the
// sole home of such things — and is never rendered by anything in this package.
package channels
