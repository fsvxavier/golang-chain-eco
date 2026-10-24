---
name: webapp-testing
description: Toolkit for interacting with and testing local web applications using Playwright. Supports verifying frontend functionality, debugging UI behavior, capturing browser screenshots, and viewing browser logs.
license: Complete terms in LICENSE.txt
---

# Web Application Testing

Test a local web application with Playwright. The approach below is
independent of the language the script is written in.

## Decision tree

- Static HTML: read the HTML file to identify selectors, then write the test
  against them. If that fails or is incomplete, treat it as dynamic.
- Dynamic web app, server not running: start it first, then test.
- Dynamic web app, server running: reconnaissance, then action.

## Reconnaissance-then-action

1. Navigate and wait for the network to be idle (`networkidle`), so the
   JavaScript has run before you look at the DOM.
2. Inspect the rendered state: take a full-page screenshot, read the page
   content, list the buttons, links and inputs.
3. Identify selectors from the rendered state.
4. Execute the actions with the discovered selectors and assert the outcome.

```python
from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)  # always headless
    page = browser.new_page()
    page.goto('http://localhost:5173')
    page.wait_for_load_state('networkidle')     # critical for dynamic apps
    # ... actions and assertions
    browser.close()
```

## Pitfall

Do not inspect the DOM of a dynamic app before waiting for `networkidle`.

## Best practices

- Use descriptive selectors: `role=`, `text=`, labels, ids; CSS as a fallback.
- Add explicit waits (`wait_for_selector`) rather than fixed sleeps.
- Always close the browser when done.
- Capture console logs during the run; a console error is a finding.
