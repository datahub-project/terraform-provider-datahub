# ISO 20022 + FIBO Demo Runbook

A walkthrough script for demonstrating DataHub against the ISO 20022 financial-messaging pipeline this example builds: 793 message types emitted as Kafka topics, PostgreSQL tables, and Looker views, enriched with FIBO (Financial Industry Business Ontology) domains and glossary terms, column-level semantic tagging, and DataHub Observe data-quality assertions.

Every navigation path and search term below was verified against a live DataHub Cloud instance. Counts are accurate as of the last tagging/emit run; treat them as approximate (they move with each re-run).

> **Setup note.** The whole environment is built by `make iso-all` (emit entities, generate assertions config, LLM tagging), then `terraform apply` (FIBO domains/glossary + assertions), then `make iso-assertion-results` (synthetic assertion run history so DataHub Observe shows live pass/fail status). See the [README](README.md) for the build steps. This document is the *presenting* guide, not the build guide.

## Demo threads at a glance

| # | Start here (search / navigation) | What it proves | Detail |
|---|---|---|---|
| 1 | Glossary search **"Legal Entity Identifier"** -> term -> *Related Entities* | A second ISO standard (ISO 17442 / LEI) surfaces inside ISO 20022 messages; FIBO is the connective tissue. ~237 related datasets. | [Thread 1](#thread-1-the-lei-cross-standard-hero) |
| 2 | Dataset search **"credit transfer"** -> `fito_ficustomer_credit_transfer` -> *Columns* | Column-level business semantics: `Dbtr` -> Debtor, `Cdtr` -> Creditor, agent chain -> Financial Institution / Account. | [Thread 2](#thread-2-column-level-semantics-on-a-payment) |
| 3 | Glossary search **"Account"** / **"Financial Institution"** / **"Debtor"** -> *Related Entities* | Business-term-first discovery: a non-technical user finds every asset carrying a concept without knowing table names. | [Thread 3](#thread-3-business-term-driven-discovery) |
| 4 | Dataset *Quality* tab (clean: `fito_ficustomer_credit_transfer`; failing: `bank_to_customer_statement`, `securities_settlement_transaction_instruction`, `customer_credit_transfer_initiation`) | DataHub Observe / governance-as-code: 5 assertion types across the estate, all defined in Terraform, with live pass/fail status and a deliberate trio of failures to talk through. | [Thread 4](#thread-4-data-quality--datahub-observe) |
| 5 | Dataset `fito_ficustomer_credit_transfer` -> *Lineage* | End-to-end 3-tier lineage: Kafka topic -> PostgreSQL table -> Looker view, generated as code. | [Thread 5](#thread-5-end-to-end-lineage) |
| 6 | Browse **Glossary** / **Domains** -> FIBO tree | Taxonomy-as-code: the entire FIBO ontology (113 domain nodes, ~1,440 terms) provisioned via Terraform. | [Thread 6](#thread-6-fibo-taxonomy-as-code) |

Base URL for all links below: your DataHub instance (the `DATAHUB_GMS_URL` host, e.g. `https://your-instance.acryl.io`). The paths are relative, so prepend your own host.

---

## Thread 1: the LEI cross-standard hero

**The moment.** Most people think of ISO 20022 as "the payments messaging standard." Searching the glossary for *Legal Entity Identifier* reveals that a *different* ISO standard - ISO 17442, the LEI - is embedded throughout these messages, and FIBO is what makes that visible.

**Steps**

1. Top search bar -> switch to **Glossary Terms** -> type `Legal Entity Identifier`.
2. Open the term **legal entity identifier** (`urn:li:glossaryTerm:tf-fibo-be-legal-entity-identifier`).
3. Open the **Related Entities** tab.

**What you see**

- ~**237** related datasets (roughly 120 Kafka topics + 117 PostgreSQL tables) carry this term.
- The term sits in the FIBO **Business Entities (BE)** domain, sourced from the EDM Council ontology - not hand-authored.

**Talk track.** "The LEI is a 20-character ISO 17442 code that uniquely identifies a legal party to a financial transaction. It shows up across payments, securities, and FX messages. Because we tagged these assets with the FIBO glossary, one search answers 'where does legal-entity identification appear in our estate?' - across 237 assets and three platforms - without anyone memorising a schema."

**Direct link**

```
/glossaryTerm/urn:li:glossaryTerm:tf-fibo-be-legal-entity-identifier/Related Entities
```

---

## Thread 2: column-level semantics on a payment

**The moment.** ISO 20022 field names are terse (`Dbtr`, `Cdtr`, `IntrmyAgt1`). DataHub shows the FIBO business meaning right on the column.

**Steps**

1. Top search bar -> **Datasets** -> `credit transfer`.
2. Open **`fito_ficustomer_credit_transfer`** (the `postgres` one - this is ISO 20022 **pacs.008**, the cross-border credit transfer / SWIFT MT103 equivalent).
3. Open the **Columns** tab.
4. Scroll to the transaction block `FIToFICstmrCdtTrf.CdtTrfTxInf.*`.

**What you see**

- 62 columns (the full message schema).
- 15 columns carry FIBO glossary terms inline, including:
  - `...CdtTrfTxInf.Dbtr` -> **Debtor**
  - `...CdtTrfTxInf.Cdtr` -> **Creditor**
  - `...CdtTrfTxInf.InstgAgt` / `InstdAgt` / `IntrmyAgt1..3` -> **Financial Institution**
  - `...DbtrAcct` / `CdtrAcct` / `IntrmyAgt1Acct` -> **Account**

**Talk track.** "`Dbtr` and `Cdtr` mean nothing to a business analyst. Here, every meaningful column is mapped to its FIBO concept - Debtor, Creditor, the chain of intermediary agents, the accounts. An analyst can read this table without an ISO 20022 reference manual open in another tab."

**Direct link**

```
/dataset/urn:li:dataset:(urn:li:dataPlatform:postgres,payments_db.public.fito_ficustomer_credit_transfer,PROD)/Columns
```

---

## Thread 3: business-term-driven discovery

**The moment.** Flip the search around: start from a business concept, land on every technical asset that embodies it.

**Steps & counts** (Glossary Terms search -> open term -> *Related Entities*)

| Search term | Term URN suffix | Related datasets |
|---|---|---|
| `Account` | `tf-fibo-fbc-account` | ~153 |
| `Financial Institution` | `tf-fibo-fbc-financial-institution` | ~92 |
| `Debtor` | `tf-fibo-fbc-debtor` | ~53 |
| `Creditor` | `tf-fibo-fbc-creditor` | ~53 |

**Talk track.** "A data steward asks: 'which of our systems touch customer account data?' They don't need to know that the answer lives in `DbtrAcct` and `CdtrAcct` columns of a pacs.008 table. They search the term Account and DataHub returns all 153 assets. This is the difference between a data catalog and a searchable data estate."

**Contrast (optional).** Searching **Datasets** for `debtor` returns ~55 noisy hits (it matches field content). Searching **Glossary Terms** for `Debtor` returns exactly **1** precise concept that then fans out to its related assets. Use this to make the point that *governed* terms beat free-text search.

---

## Thread 4: data quality / DataHub Observe

**The moment.** Governance-as-code: data-quality rules defined in Terraform, evaluated and visible in DataHub - with most assertions green and a deliberate handful red to talk through incident detection.

**Steps - the clean table first**

1. Open **`fito_ficustomer_credit_transfer`** (pacs.008 PostgreSQL table).
2. Open the **Quality** tab (Validations / Assertions).

**What you see** - 8 assertions on this one table, spanning all five types, all **passing**:

| Type | Example rule |
|---|---|
| **Schema** | Schema stability for pacs.008 - fails if columns are added, removed, or retyped. |
| **Volume** | Volume gate: table must hold >= 50,000 records (a drop signals a pipeline gap). |
| **Field** | `Message ID`, `Number of Transactions`, and `Total Interbank Settlement Amount` must never be null. |
| **SQL** | Zero tolerance for null message IDs; transaction count must always be present. |
| **Freshness** | Data must refresh within 24 hours; a longer silence means the feed stalled. |

**Scale.** Across the 26 representative tables there are **208** assertions (26 schema + 26 volume + 78 field + 52 SQL + 26 freshness), all generated from `assertions_config.json` and applied via `for_each` in `assertions.tf`. Each carries an evaluation history so the Observe view shows a status trend, not just a definition.

**Then the failures - three different tables, three different types, three different stories.** Search each table, open its Quality tab:

| Dataset (search term) | Type | Status | Talk track |
|---|---|---|---|
| `bank_to_customer_statement` (camt.053) | **Volume** | FAIL | "Statement volume dropped to ~430 rows against a 1,000 floor - a partial feed; bank-statement reconciliation is at risk." |
| `securities_settlement_transaction_instruction` (sese.023) | **Freshness** | FAIL | "Settlement instructions have not refreshed in over 24 hours - the upstream feed has stalled." |
| `customer_credit_transfer_initiation` (pain.001) | **SQL** | FAIL | "37 payment initiations arrived with null message IDs - an integrity violation that breaks downstream matching." |

Each failing assertion shows a recent **regression** (passing, then failing in the last couple of evaluations), so the story is "this was healthy and just broke," not "this was always red."

**Talk track.** "Every one of these rules is a Terraform resource - the data team reviews data-quality policy as a pull request, the same way they review infrastructure. The pacs.008 backbone is clean across all five assertion types. But the platform has caught three real problems: a volume drop on bank statements, a stalled securities-settlement feed, and an integrity violation on customer payments - each one an incident a governance team would act on."

> **Where to look.** The per-dataset **Quality** tab is the reliable surface and reads directly from the assertion results. The estate-wide **Observe -> Assertions** summary (`/observe/datasets/assertions`) aggregates the same data but depends on search indexing, which can lag a few minutes on a busy instance.

> **How the results got there.** On these synthetic datasets the monitors have no profile/audit data to evaluate against, so the assertions would otherwise show "awaiting evaluation." `scripts/iso20022/emit_assertion_results.py` writes synthetic `assertionRunEvent` history (mostly SUCCESS, with the three failures above) so the demo shows live status. Schema/volume/field/freshness assertions are **DataHub Cloud only**; SQL assertions are PASSIVE (defined, and evaluate when a database connection is attached).

**Direct link (clean hero)**

```
/dataset/urn:li:dataset:(urn:li:dataPlatform:postgres,payments_db.public.fito_ficustomer_credit_transfer,PROD)/Quality
```

---

## Thread 5: end-to-end lineage

**The moment.** One message type, traced across the whole platform.

**Steps**

1. Open **`fito_ficustomer_credit_transfer`** (PostgreSQL).
2. Open the **Lineage** tab (or the lineage graph view).

**What you see**

```
iso20022.pacs.pacs.008.001.14   ->   fito_ficustomer_credit_transfer   ->   fito_ficustomer_credit_transfer_view
        (Kafka topic)                      (PostgreSQL table)                       (Looker view)
```

- **Upstream:** the Kafka topic the message lands on.
- **Downstream:** the Looker view that exposes it for analytics.

**Talk track.** "This is the journey of a single payment message: it arrives on a Kafka topic, is persisted to a PostgreSQL table, and is surfaced in a Looker view for analysts. The lineage - including the platform hops - is generated as code alongside the entities, so it never drifts from reality."

**Direct link**

```
/dataset/urn:li:dataset:(urn:li:dataPlatform:postgres,payments_db.public.fito_ficustomer_credit_transfer,PROD)/Lineage
```

---

## Thread 6: FIBO taxonomy as code

**The moment.** The entire FIBO ontology, provisioned by Terraform as a navigable taxonomy.

**Steps**

1. Open **Govern -> Glossary** (or **Govern -> Domains**).
2. Expand the FIBO tree: domain (e.g. *Securities (SEC)*) -> module (e.g. *Debt*) -> leaf (e.g. *Bonds*) -> terms (*bond*, *callable bond*, ...).

**What you see**

- **113** domain nodes (root + 7 top-level domains + 29 modules + 76 leaves).
- **~1,440** glossary terms drawn from FIBO `owl:Class` definitions, each with the EDM Council definition as its description.

**Talk track.** "The whole financial-industry business vocabulary - thousands of formally-defined terms - is here as a governed glossary, sourced from the EDM Council's open ontology and provisioned with a single Terraform apply. It is the shared language that the column-level tags in the other threads point back to."

---

## Honest caveats (so you are not surprised live)

- **Domain auto-classification is approximate.** Datasets were assigned a single FIBO *domain* by an LLM scoring field content, and the assignment is noisy - e.g. some payment tables land in *Business Entities* or *Indices and Indicators* rather than a clean "Payments" domain (FIBO has no payments domain; the nearest is *Financial Business and Commerce*). **Lead with glossary-term navigation (Threads 1-3), which is precise; treat domain browsing (Thread 6) as "explore the taxonomy," not "see the clean dataset classification."**
- **No PII tags on the financial data.** This pipeline is not classified for PII/sensitive data, so there is no PII-tagging thread here. If asked, frame PII as a separate classification capability (demonstrated in other datasets), not part of this dataset.
- **Counts move per run.** Related-asset counts and tagged-column counts shift slightly each time tagging is re-run; the numbers above are indicative, not contractual.

## Quick-reference: verified search terms

| Type | Term | Result |
|---|---|---|
| Glossary | `Legal Entity Identifier` | 71 matching terms; lead term has ~237 related assets |
| Glossary | `Account` | term with ~153 related assets |
| Glossary | `Financial Institution` | term with ~92 related assets |
| Glossary | `Debtor` / `Creditor` | 1 precise term each, ~53 related assets |
| Glossary | `settlement` | 13 FIBO terms |
| Glossary | `collateral` | 6 FIBO terms |
| Dataset | `credit transfer` | 15 assets (pacs.008 across kafka/postgres/looker) |
| Dataset | `pacs.008` | 63 assets (full pacs family) |
