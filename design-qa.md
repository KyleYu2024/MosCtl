# Design QA

- Source visual truth: user annotations in `/var/folders/wq/4gmlw0zx7njd4xz4tlq107140000gn/T/codex-clipboard-07f2437c-9ad6-4bb3-bdea-1b62c65585de.png`, `/var/folders/wq/4gmlw0zx7njd4xz4tlq107140000gn/T/codex-clipboard-dc91f3d7-0062-46b1-b1cb-47a20424f7aa.png`, `/var/folders/wq/4gmlw0zx7njd4xz4tlq107140000gn/T/codex-clipboard-c3151b95-4443-47b7-b7b6-7b04275d4a06.png`, `/var/folders/wq/4gmlw0zx7njd4xz4tlq107140000gn/T/codex-clipboard-c85efc71-9a59-44c7-9999-ff4f2b2b39b4.png`, `/var/folders/wq/4gmlw0zx7njd4xz4tlq107140000gn/T/codex-clipboard-d1406165-69f1-4220-a49b-036889d16db4.png`, `/var/folders/wq/4gmlw0zx7njd4xz4tlq107140000gn/T/codex-clipboard-cd990c4c-a968-4161-90c4-af85520a3776.png`, and `/var/folders/wq/4gmlw0zx7njd4xz4tlq107140000gn/T/codex-clipboard-6480207c-33cd-451a-9df8-6f83d91912e5.png`
- Implementation: `http://127.0.0.1:9099/` (Codex in-app browser)
- Implementation screenshot evidence: in-app browser captures of logged-out, authenticated desktop, and authenticated mobile states
- Viewports: 1440 x 900 and 390 x 844 CSS px, device scale factor 1
- State: authenticated, first rule set selected, no unsaved changes; overflow was also tested with 220 unsaved rules

**Full-view comparison evidence**

The subtraction pass is visible in both states. The login screen contains only the enlarged MOSCTL brand, credentials, login action, and error region. The authenticated screen starts directly with the rule workspace; the former page title/description block has been removed. The workspace is a flat two-column composition without an outer frame, vertical divider, or nested outlined panels.

The duplicate active-rule heading has been removed. Save-and-apply now sits in a fixed editor footer and remains visible at the bottom of the viewport. The shell is locked to the viewport; extra rule content scrolls only inside the textarea.

**Focused region comparison evidence**

- Login: the two annotated copy blocks are absent and MOSCTL is a 30 px uppercase display mark.
- Header: the logout text button is replaced by a red Tabler logout icon with a 40 px target, hover state, tooltip, and accessible name.
- Workspace: selection uses a quiet filled background instead of an outline. Add and delete are performed directly in the large editor; the separate add row and delete-selection control are absent. Search remains as the only secondary control.
- Scroll behavior: at 1440 x 900, body client height and scroll height were both 900 px. With 220 rules, only the editor overflowed while the page remained fixed. In the final desktop layout, the textarea ended at 810 px and the save action at 864 px, both inside the 900 px viewport.

**Required fidelity surfaces**

- Fonts and typography: enlarged uppercase branding is clear; editor hierarchy remains readable after removal of the introductory heading.
- Spacing and layout rhythm: removing the intro block and framed container reduces density; the fixed-height shell keeps primary actions in a single view.
- Colors and visual tokens: near-black surfaces and warm gold emphasis remain consistent; the logout icon uses the existing muted-red destructive token.
- Image quality and asset fidelity: the logout control uses a local Tabler Icons SVG asset under its MIT license; no text glyph or CSS drawing substitutes for the icon.
- PWA surfaces: manifest metadata, standalone display, theme colors, Apple metadata, safe-area spacing, 192/512/maskable/app-touch icons, and an app-shell-only service worker are present. Auth and rule APIs are explicitly excluded from caches.
- Copy and content: only the user-requested explanatory blocks were removed. Rule names, descriptions, filenames, counts, feedback, and action labels remain intact.

**Findings**

- No actionable P0/P1/P2 visual or responsive issues remain.

**Interaction verification**

- Login succeeded with `USERNAME` and `PASSWORD` supplied to the preview process.
- The red icon-only logout control ended the session and returned to the login form; re-login succeeded.
- All four sample rule counts loaded. Direct editing enabled the save action, saving succeeded, direct deletion and a second save restored the file, and the browser console reported no warnings or errors.
- Manifest, service worker, logout icon, and all PWA icon routes return their declared content types; the service worker is scoped to `/`.
- Search and editor measured exactly 862 px wide at 1440 x 900. After stopping the preview server, a browser reload still rendered the cached login shell, confirming offline fallback; API data was unavailable as intended.
- API, asset delivery, authentication, and filesystem behavior are covered by Go tests.

**Comparison history**

- Pass 1: removed the two login copy regions, removed the authenticated-page intro, enlarged MOSCTL, and flattened the workspace.
- Pass 2: replaced the logout text button with the requested red icon and verified its interaction and accessible label.
- Pass 3: moved save-and-apply to the rule heading and constrained overflow to the rule editor; verified with 220 rules on desktop and mobile.
- Pass 4: removed the duplicate active-rule heading and placed save-and-apply in the fixed editor footer.
- Pass 5: removed the redundant add row and delete-selection control; verified direct add/delete in the large editor and two successful saves.
- Pass 6: expanded search to the exact editor width and placed match feedback inside the field so alignment remains unchanged.
- Pass 7: added complete installable PWA metadata, platform icons, safe-area behavior, network status, and an API-safe offline shell.
- Pass 8: replaced the temporary M icon with the approved deeper-blue circular rising-arrow logo, using transparent rounded corners for standard icons and an opaque full-bleed source for maskable and Apple icons. The service-worker cache version was advanced so installed clients refresh the artwork.
- Pass 9: added a single-source `v0.5.3` version badge to the login card and authenticated header; both rendered states were checked with no wrapping or console errors.
- Pass 10: removed the `MosDNS Rule Control` subtitle, changed iOS standalone status-bar mode from overlaying `black-translucent` to `black`, and restored safe-area padding inside the mobile media rule so device status content cannot overlap the header.
- Final browser pass found no remaining P0/P1/P2 issues.

**Implementation Checklist**

- [x] Remove annotated login copy.
- [x] Enlarge MOSCTL.
- [x] Remove the authenticated-page intro block.
- [x] Reduce workspace borders and dividers.
- [x] Replace logout text with a red icon.
- [x] Keep save-and-apply visible.
- [x] Scroll only the rule editor for long lists.
- [x] Edit additions and deletions directly in the rule editor.
- [x] Support installable standalone PWA behavior without caching credentials or rule APIs.
- [x] Preserve desktop and mobile usability.

**Follow-up Polish**

- None required for this pass.

final result: passed
