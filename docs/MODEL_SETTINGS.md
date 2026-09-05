# Desktop model settings

Open **Settings → Models → Access**, or choose **Manage models** from the chat model menu.

Settings fills the application window. Use **Back to workspace** to return to the same workspace; reopening settings remembers the last category during this app session. Escape closes a child dialog or menu, but does not exit the settings page.

Select a saved provider in the left column to edit its connection and models. Switching providers keeps open forms' drafts. **Save** applies the form; **Cancel** discards it. Closing settings discards unsaved forms.

## Add a provider

- **Recommended presets:** open the compact selector and search by name. Select a preset, enter its API key, then add it. Installed/conflicting presets retain their status. Products with several protocol routes remain grouped; individual routes are available under advanced route settings.
- **Custom provider:** enter a name, protocol, Base URL, and key. The form previews the request URL. Special gateway paths can use **Override request URL** under compatibility settings; it takes precedence over Base URL. Existing exact URLs and legacy Chat URLs retain their request behavior.

An empty key field on an existing provider keeps the saved credential. Use the separate clear-key action to remove it.

## Manage models

**Refresh models** opens a searchable selection dialog. Existing models are checked and locked. **Add selected models** appends only selected new models to the form; save the provider to apply them. Discovery failure does not prevent manual entry.

**Add manually** and each row's edit button open a model dialog. Enter the exact model ID, optional context/output limits, and image capability. Empty limits inherit defaults. Output `-1` omits optional limits; protocols requiring a limit still use their fallback. Image **Auto** uses metadata; overrides must match the endpoint's actual capability. Editing limits preserves existing reasoning overrides.

Deleting a model stages its removal; cancel the provider form to undo it. The default model stays unchanged if it remains in the list, otherwise the first remaining model becomes the provider default.

**Test model** sends a short request through the selected adapter with no conversation history or tools. It uses the current form, including an unsaved key, and may incur a small provider charge. Neither testing nor discovery saves credentials or configuration. Editing the connection invalidates previous test results. Success confirms that the request was accepted, not every model capability.

The chat menu groups saved, available models by provider and displays context/image metadata when available. Editing model settings does not change system prompts or tool schemas.

## Windows window behavior

Windows settings keeps the minimize, maximize/restore, and close-window buttons at the top right. The title strip and caption buttons share a 48 CSS px height and the settings background, independently of the underlying Workbench or Creation layout. The empty title strip is draggable; the Back button, form controls, and caption buttons are not drag regions.

**Back to workspace** only leaves settings. **Close window** retains the configured close behavior in General settings. A model dialog's close button only dismisses that dialog.

### Windows reference research and validation boundary (2026-09-05)

- Reference: ZCode for Windows.
- Installed application code confirms native Windows caption buttons, a transparent title-bar overlay with a 48px baseline updated with UI zoom, a full-page settings component, separate return and window-close actions, and configurable close-to-tray behavior.
- Reasonix retains Wails window controls and applies shared caption geometry and colors to settings; it does not introduce Electron window code.
- Reasonix's Windows browser preview was checked for caption geometry, colors, and Workbench/Creation transitions. Settings navigation and child-dialog tests also passed.
- Native Windows dragging, system scaling, maximize, and tray acceptance checks remain unverified. Installed-code inspection does not replace those checks.
