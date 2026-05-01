# break-a-leg

`break-a-leg` is a local trader-assistance app for premarket HOD burst investigation. It watches a configured ticker list with Massive.com market data, detects sensitive premarket high-of-day burst conditions, checks RTPR press releases, asks Perplexity Sonar in SEC search mode for trader-focused news quality analysis, generates a short OpenAI TTS headline, and shows the result as live web alert cards.

It is not a fully automated trading bot. The LLM never places trades. Trade buttons create user-selected order intents that pass through risk guardrails before reaching a broker adapter. The default broker is dummy/simulated mode with trading disabled.

## Setup

1. Create your local env file:

```sh
cp env.example .env
```

2. Fill in any keys you plan to use:

```dotenv
MASSIVE_API_KEY=
RTPR_API_KEY=
OPENAI_API_KEY=
PPLX_API_KEY=
```

3. Edit `watchlist.yaml`:

```yaml
tickers:
  - ABCD
  - XYZ
  - MNO
```

4. Run the app:

```sh
go run .
```

or:

```sh
make run
```

The default UI is `http://127.0.0.1:8087`.

## Configuration

`config.yaml` controls server host/port, premarket session times, watchlist files, Massive reconnect behavior, burst thresholds, RTPR freshness windows, Perplexity LLM settings, OpenAI TTS settings, risk guardrails, broker mode, and trade buttons.

The default HOD burst rule is:

- session is premarket, default `04:00` to `09:30` ET
- ticker is in the watchlist
- premarket cumulative volume is at least `20,000`
- percent change is at least `10%`, measured from the previous market close
- at least `2` new HOD events occur within `60` seconds
- full workflow cooldown is `5` minutes per ticker

The optional soft trigger is visual-only by default when percent change is between `5%` and `10%` with the same volume and HOD-window rules.

To change thresholds, edit the `burst:` section in `config.yaml`.

If an alert initially has no RTPR article, the app rechecks RTPR once per minute at the `:30` second. If a later article appears with `created_at` after the alert time, the existing card is upgraded and the normal LLM/TTS workflow runs.

The gappers tab uses the same previous-market-close reference for `% change`, and premarket volume is seeded from Massive 1-minute aggregates starting at `04:00` ET before live ticks continue updating the numbers.

## LLM And TTS

The LLM provider uses Perplexity Sonar in SEC search mode so outside context from SEC filings and related sources can be used in the analysis:

```yaml
llm:
  provider: "perplexity"
  model: "sonar-pro"
  search_mode: "sec"
  prompt_file: "prompts/news_analysis.md"
  timeout_seconds: 90
```

The TTS provider still uses OpenAI speech generation:

```yaml
tts:
  model: "gpt-4o-mini-tts"
  voice: "alloy"
  output_format: "mp3"
```

Disable LLM or TTS by setting `enabled: false` in the relevant section.

## Trade Buttons And Dummy Broker

Trade buttons are configured in `config.yaml` under `trade_buttons:`. By default:

```yaml
broker:
  provider: "dummy"
  dummy_mode: true
  trading_enabled: false
```

With `trading_enabled: false`, clicks are logged and shown in the UI as blocked by config. This is intentional. Risk checks always run before any broker adapter call.

Risk settings include max shares, max notional, duplicate-click cooldown, allowed order types, alert staleness, watchlist enforcement, and optional spread checks.

## Local Data

The app persists review artifacts under `data/`:

- `data/alerts/` alert cards and updates
- `data/news/` RTPR lookup results
- `data/llm/` Markdown analysis
- `data/audio/` generated TTS files
- `data/trades/` trade intent records
- `data/risk/` risk decisions
- `data/logs/` application logs

## Future Broker Adapter

Broker execution is isolated behind:

```go
type Broker interface {
    PlaceOrderIntent(ctx context.Context, intent OrderIntent) (OrderResult, error)
}
```

`DummyBroker` is the only implementation included. A future `SchwabBroker` can be added without changing burst detection, news, LLM, TTS, UI, or risk modules.

## Tests

Run:

```sh
make test
```

Current tests cover HOD burst detection, cooldown behavior, risk duplicate/notional blocks, and news freshness classification.
