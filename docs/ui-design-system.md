# UI design system

Issue: #133

## Information first

- Headings name the current function: カレンダー / 組織の公開状態 / 受信した依頼 / 送信した依頼 / 操作履歴.
- Explain available operations and data boundaries in body text. Do not use decorative English eyebrows, oversized serif headings or abstract taglines.
- English product/provider names and technical identifiers remain where meaningful.
- Keep incomplete OAuth verification, sync limitations and AI consent disclosures visible.

## Surfaces and depth

The application frame is dark blue-grey, the navigation is recessed grey, and work surfaces are white. Green identifies primary actions and available time. Amber and red accompany text labels, never replace them.

Use continuous lists, shared dividers and calendar lanes rather than independently floating cards. Flat borders, inset selection edges and short, hard shadows establish depth. No blurred glass, gradient backgrounds, tilted samples or soft card shadows.

Base colors are defined in web/src/styles.css and hosting/public/styles.css. A Python test checks their equivalence. Keep the static Hosting site limited to its existing three files and strict content security policy.

## Type and controls

- Desktop page title: 28px; mobile: 24px. Public entry title: 28–36px.
- Section headings: 16–20px; primary content: 13–15px.
- Small text is reserved for timestamps, privacy labels and supplemental details.
- Button corners are 3px. Circular avatars remain identity indicators, not content containers.
- All navigation and segmented controls expose selection with aria-current or aria-pressed.
- Focus outlines remain visible. Reduced-motion preference disables pressed-button movement.

## Responsive behavior

At 900px and below, left navigation becomes a sticky horizontal strip. Calendar lanes stack, and request/person rows become a single-column list. No operation is removed at mobile sizes; sharing-rule access remains visible.

## Verification

Run from web:

    npm test
    npm run lint
    npm run build
    npx playwright install chromium
    npm run test:ui

The browser suite checks 1440, 1024, 390 and 320px widths, calendar controls, dialogs, populated organization/request/audit views, the public entry and production sign-in. API responses are synthetic and intercepted; no real account or calendar is modified. Screenshots are written to ignored web/test-results for visual review.

Browser fixture tests verify presentation and existing UI wiring, not Google OAuth approval, real calendar synchronization or backend correctness.
