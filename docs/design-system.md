# Portal design

October 9, 2026. The visual direction takes inspiration from Cursor's restrained
website: generous spacing, warm neutral surfaces, precise type, quiet borders
and restrained olive accents. Mist keeps its own research workflow and identity.

## Shared decisions

- Warm off-white page canvas, white content surfaces, muted sidebar, dark ink.
- Self-hosted Manrope Latin variable font; no runtime external font request.
  The SIL Open Font License is included under `web-interface/public/fonts`.
- A fixed desktop sidebar with navigation, workspace and account controls.
  At narrow widths it becomes a compact top navigation.
- Interactive components use shared icon headings, clear boundaries, primary/secondary
  button hierarchy, hover/focus/pressed feedback and explicit loading/empty states.
- Compute is selected with native keyboard-accessible radio cards; workload modes
  use segmented radio controls. Job settings are grouped into numbered sections. Discrete device sliders
  are paired with exact numeric inputs, so precision is never lost.
- Dataset uploads support click-to-select and drag/drop; common/member folders
  are selectable tiles, with explicit read-only sharing indicators.
- Jobs uses clickable status metrics, collapsible hardware availability, a focused
  submission dialog,
  and compact job-history rows. Each accessible row button expands details;
  History has 10/25/50 rows per page, search, status filters and Previous/Next
  controls. There is no infinite scrolling. Logs/Files are secondary actions. Expanded content retains actual runtime data.
- Overview introduces the next experiment and links directly into jobs/datasets.
  Team/account administration uses compact team cards and searchable, paginated
  member rows. Team settings use Teammates/Limits/Sharing tabs; account settings
  separate personal work from member administration. Creation/reset actions open
  focused dialogs rather than filling the page with permanently open forms.
- Layouts stack on smaller screens, tables scroll within their containers,
  long names wrap/truncate appropriately, and focus states remain visible.
  Motion respects reduced-motion preferences. Status always includes text.

## Interaction principles

- Each feature has one primary action. Details and advanced settings appear on
  demand; explanatory copy is brief and placed next to the relevant decision.
- Shared Picker replaces legacy selects with a search field, clear selected state,
  keyboard navigation and no-match feedback. Long account/image labels wrap in
  options; the current selection truncates without widening the form.
- Native Modal provides a focus trap, Escape/backdrop close and focus return.
  File transfer disables closing until finished or explicitly cancelled. Dialogs
  scroll within the screen and retain a visible primary action for job submission.
- Shared Tabs has one tab stop, arrow-key navigation, Home/End and labelled panels.
  Buttons, radio cards, disclosure controls and file drop targets preserve native
  semantics; icons supplement text rather than hiding the action's meaning.
- Dataset library rows show name, location, file count, size and state. IDs and
  checksums are optional details. The upload ceiling reflects actual free space;
  the storage bar explains why a larger allocation may be needed.
- Use sliders for counts/allocations with meaningful bounds. Keep exact inputs for
  resource quantities and runtime, which may need precise values and units.

## Source map

| Concern | Source |
|---|---|
| Colors, type, spacing, responsive layout | `web-interface/src/styles.css` |
| Navigation, workspace/account controls | `components/Navbar.tsx`, `teams.tsx` |
| Overview | `routes/dashboard.tsx` |
| Job workbench/history | `components/JobsPage.tsx`, `components/jobs/JobCard.tsx` |
| Submission and clear validation | `components/jobs/JobSubmissionForm.tsx` |
| Searchable inputs, dialogs and tabs | `components/Picker.tsx`, `components/Modal.tsx`, `components/Tabs.tsx` |
| Shared surfaces/actions | `components/Card.tsx`, `components/Buttons.tsx` |
| Team storage/admin | `components/TeamStorageBrowser.tsx`, `components/TeamsPage.tsx` |

Component paths above are relative to `web-interface/src`. Prefer these shared
styles/components over inventing a new visual treatment for each feature.

Real browser verification covered administrator/member actions, row expansion,
file upload/download, container submission and password reset at desktop and
390 px mobile widths. Nine React tests also cover job/history behavior and keyboard control flows.
Screenshots and reports are linked in the rollout checklist.
