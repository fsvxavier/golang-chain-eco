# Third-party notices

The persona folders under `skills/<role>/references/` carry copies of third-party agent skills, fetched with the `skills` CLI (`npx skills add <owner/repo@skill>`) and pinned in `skills.lock` (SHA-256 of each embedded file). Each role folder has its own copy.

**Provenance.** The copies are trimmed and sanitised: examples and boilerplate were cut to fit the size budget (8 KB per file, 24 KB per role), and installer commands, remote-fetch instructions, script invocations for scripts that are not vendored, and tool permission grants (`allowed-tools`) were removed. They are not the upstream files. These copies were taken from the ones already vendored in `full_team_front/skills` (same files, same hashes), keeping the generic skills; the front-end ones do not apply here. The skill `user-story` from deanpeters/product-manager-skills (CC BY-NC-SA 4.0) is not redistributed; `user-stories` from phuryn/pm-skills (MIT) is used instead.

## Sources

| Repository | Skills | Licence | Copyright holder as stated in the source |
|---|---|---|---|
| https://github.com/affaan-m/ecc | `contract-first` | MIT | Copyright (c) 2026 Affaan Mustafa |
| https://github.com/addyosmani/agent-skills | `planning-and-task-breakdown`, `performance-optimization` | MIT | Copyright (c) 2025 Addy Osmani |
| https://github.com/mattpocock/skills | `code-review` | MIT | Copyright (c) 2026 Matt Pocock |
| https://github.com/phuryn/pm-skills | `user-stories` | MIT | Copyright (c) 2026 Pawel Huryn |

## Licence texts

### MIT

Applies to every source above marked MIT. Each holder's copyright line is the one in the table.

```text
MIT License

Copyright (c) <year> <holder as in the table>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
