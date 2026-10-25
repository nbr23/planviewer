# Dark mode for settings pages

## Steps

1. Replace the 23 hard-coded colors under `src/settings/` with tokens from `theme.css`
2. Add `[data-theme="dark"]` values for every token
3. Theme picker in **Settings → Appearance**: system, light, dark, persisted in `localStorage`
4. Follow `prefers-color-scheme` when set to system

## Open questions

- Do charts follow the theme, or stay light for exports?
