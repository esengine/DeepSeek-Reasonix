# Changelog

## [v1.1.0](https://github.com/getlantern/systray/tree/v1.1.0) (2020-11-18)

[Full Changelog](https://github.com/getlantern/systray/compare/v1.0.5...v1.1.0)

**Merged pull requests:**

- Add submenu support for Linux [\#183](https://github.com/getlantern/systray/pull/183) ([fbrinker](https://github.com/fbrinker))
- Add checkbox support for Linux [\#181](https://github.com/getlantern/systray/pull/181) ([fbrinker](https://github.com/fbrinker))
- fix SetTitle documentation [\#179](https://github.com/getlantern/systray/pull/179) ([delthas](https://github.com/delthas))

## [v1.0.5](https://github.com/getlantern/systray/tree/v1.0.5) (2020-10-19)

[Full Changelog](https://github.com/getlantern/systray/compare/v1.0.4...v1.0.5)

**Merged pull requests:**

- start menu ID with positive, and change the type to uint32 [\#173](https://github.com/getlantern/systray/pull/173) ([joesis](https://github.com/joesis))
- Allows disabling items in submenu on macOS [\#172](https://github.com/getlantern/systray/pull/172) ([joesis](https://github.com/joesis))
- Does not use the template icon for regular icons [\#171](https://github.com/getlantern/systray/pull/171) ([sithembiso](https://github.com/sithembiso))

## [v1.0.4](https://github.com/getlantern/systray/tree/v1.0.4) (2020-07-21)

[Full Changelog](https://github.com/getlantern/systray/compare/1.0.3...v1.0.4)

**Merged pull requests:**

- protect shared data structures with proper mutexes [\#162](https://github.com/getlantern/systray/pull/162) ([joesis](https://github.com/joesis))

## [1.0.3](https://github.com/getlantern/systray/tree/1.0.3) (2020-06-11)

[Full Changelog](https://github.com/getlantern/systray/compare/v1.0.3...1.0.3)

## [v1.0.3](https://github.com/getlantern/systray/tree/v1.0.3) (2020-06-11)

[Full Changelog](https://github.com/getlantern/systray/compare/v1.0.2...v1.0.3)

**Merged pull requests:**

- have a workaround to avoid crash on old macOS versions [\#153](https://github.com/getlantern/systray/pull/153) ([joesis](https://github.com/joesis))
- Fix bug on darwin of setting icon for menu [\#147](https://github.com/getlantern/systray/pull/147) ([mangalaman93](https://github.com/mangalaman93))

## [v1.0.2](https://github.com/getlantern/systray/tree/v1.0.2) (2020-05-19)

[Full Changelog](https://github.com/getlantern/systray/compare/v1.0.1...v1.0.2)

**Merged pull requests:**

- remove unused dependencies [\#145](https://github.com/getlantern/systray/pull/145) ([joesis](https://github.com/joesis))

## [v1.0.1](https://github.com/getlantern/systray/tree/v1.0.1) (2020-05-18)

[Full Changelog](https://github.com/getlantern/systray/compare/1.0.1...v1.0.1)

## [1.0.1](https://github.com/getlantern/systray/tree/1.0.1) (2020-05-18)

[Full Changelog](https://github.com/getlantern/systray/compare/1.0.0...1.0.1)

**Merged pull requests:**

- Unlock menuItemsLock before changing UI [\#144](https://github.com/getlantern/systray/pull/144) ([joesis](https://github.com/joesis))

## [1.0.0](https://github.com/getlantern/systray/tree/1.0.0) (2020-05-18)

[Full Changelog](https://github.com/getlantern/systray/compare/v1.0.0...1.0.0)

## [v1.0.0](https://github.com/getlantern/systray/tree/v1.0.0) (2020-05-18)

[Full Changelog](https://github.com/getlantern/systray/compare/0.9.0...v1.0.0)

**Merged pull requests:**

- Check if the menu item is nil [\#137](https://github.com/getlantern/systray/pull/137) ([myleshorton](https://github.com/myleshorton))

## [0.9.0](https://github.com/getlantern/systray/tree/0.9.0) (2020-03-24)

[Full Changelog](https://github.com/getlantern/systray/compare/v0.9.0...0.9.0)

## [v0.9.0](https://github.com/getlantern/systray/tree/v0.9.0) (2020-03-24)

[Full Changelog](https://github.com/getlantern/systray/compare/8e63b37ef27d94f6db79c4ffb941608e8f0dc2f9...v0.9.0)

**Merged pull requests:**

- Backport all features and fixes from master [\#140](https://github.com/getlantern/systray/pull/140) ([joesis](https://github.com/joesis))
- Nested menu windows [\#132](https://github.com/getlantern/systray/pull/132) ([joesis](https://github.com/joesis))
- Support for nested sub-menus on OS X [\#131](https://github.com/getlantern/systray/pull/131) ([oxtoacart](https://github.com/oxtoacart))
- Use temp directory for walk resource manager [\#129](https://github.com/getlantern/systray/pull/129) ([max-b](https://github.com/max-b))
- Added support for template icons on macOS [\#119](https://github.com/getlantern/systray/pull/119) ([oxtoacart](https://github.com/oxtoacart))
- When launching app window on macOS, make application a foreground app… [\#118](https://github.com/getlantern/systray/pull/118) ([oxtoacart](https://github.com/oxtoacart))
- Include stdlib.h in systray\_browser\_linux to explicitly declare funct… [\#114](https://github.com/getlantern/systray/pull/114) ([oxtoacart](https://github.com/oxtoacart))
- Fix panic when resources root path is not the working directory [\#112](https://github.com/getlantern/systray/pull/112) ([ksubileau](https://github.com/ksubileau))
- Don't print close reason to console [\#111](https://github.com/getlantern/systray/pull/111) ([ksubileau](https://github.com/ksubileau))
- Systray icon could not be changed dynamically [\#110](https://github.com/getlantern/systray/pull/110) ([ksubileau](https://github.com/ksubileau))
- Preventing deadlock on menu item ClickeCh when no one is listening, c… [\#105](https://github.com/getlantern/systray/pull/105) ([oxtoacart](https://github.com/oxtoacart))
- Reverted deadlock fix \(Affected other receivers\) [\#104](https://github.com/getlantern/systray/pull/104) ([ldstein](https://github.com/ldstein))
- Fix Deadlock and item ordering in Windows [\#103](https://github.com/getlantern/systray/pull/103) ([ldstein](https://github.com/ldstein))
- Minor README improvements \(go modules, example app, screenshot\) [\#98](https://github.com/getlantern/systray/pull/98) ([tstromberg](https://github.com/tstromberg))
- Add support for app window [\#97](https://github.com/getlantern/systray/pull/97) ([oxtoacart](https://github.com/oxtoacart))
- systray\_darwin.m: Compare Mac OS min version with value instead of macro [\#94](https://github.com/getlantern/systray/pull/94) ([teddywing](https://github.com/teddywing))
- Attempt to fix https://github.com/getlantern/systray/issues/75 [\#92](https://github.com/getlantern/systray/pull/92) ([mikeschinkel](https://github.com/mikeschinkel))
- Fix application path for MacOS in README [\#91](https://github.com/getlantern/systray/pull/91) ([zereraz](https://github.com/zereraz))
- Document cross-platform console window details [\#81](https://github.com/getlantern/systray/pull/81) ([michaelsanford](https://github.com/michaelsanford))
- Fix bad-looking system tray icon in Windows [\#78](https://github.com/getlantern/systray/pull/78) ([juja256](https://github.com/juja256))
- Add the separator to the visible items [\#76](https://github.com/getlantern/systray/pull/76) ([meskio](https://github.com/meskio))
- keep track of hidden items [\#74](https://github.com/getlantern/systray/pull/74) ([kalikaneko](https://github.com/kalikaneko))
- Support macOS older than 10.13 [\#73](https://github.com/getlantern/systray/pull/73) ([swznd](https://github.com/swznd))
- define ERROR\_SUCCESS as syscall.Errno [\#69](https://github.com/getlantern/systray/pull/69) ([joesis](https://github.com/joesis))
- Bug/fix broken menuitem show [\#68](https://github.com/getlantern/systray/pull/68) ([kalikaneko](https://github.com/kalikaneko))
- Fix mac deprecations [\#66](https://github.com/getlantern/systray/pull/66) ([jefvel](https://github.com/jefvel))
- Made it possible to add icons to menu items on Mac [\#65](https://github.com/getlantern/systray/pull/65) ([jefvel](https://github.com/jefvel))
- linux: delete temp files as soon as they are not needed [\#63](https://github.com/getlantern/systray/pull/63) ([meskio](https://github.com/meskio))
- Merge changes from amkulikov to remove DLL for windows [\#56](https://github.com/getlantern/systray/pull/56) ([oxtoacart](https://github.com/oxtoacart))
- Revert "Use templated icons for the menu bar in macOS" [\#51](https://github.com/getlantern/systray/pull/51) ([stoggi](https://github.com/stoggi))
- Use templated icons for the menu bar in macOS [\#46](https://github.com/getlantern/systray/pull/46) ([stoggi](https://github.com/stoggi))
- Syscalls instead of custom DLLs [\#44](https://github.com/getlantern/systray/pull/44) ([amkulikov](https://github.com/amkulikov))
- On quit exit main loop on linux [\#41](https://github.com/getlantern/systray/pull/41) ([meskio](https://github.com/meskio))
- Fixed hide show in linux \(\#37\) [\#39](https://github.com/getlantern/systray/pull/39) ([meskio](https://github.com/meskio))
- fix: linux compilation warning [\#36](https://github.com/getlantern/systray/pull/36) ([novln](https://github.com/novln))
- Added separator functionality [\#32](https://github.com/getlantern/systray/pull/32) ([oxtoacart](https://github.com/oxtoacart))



\* *This Changelog was automatically generated by [github_changelog_generator](https://github.com/github-changelog-generator/github-changelog-generator)*
