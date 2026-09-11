// Uses the existing browser-only bridge fixtures. No native configuration access.
import React from 'react';
import {applyTheme} from '../src/lib/theme';
import {createRoot} from 'react-dom/client';
import {UpdaterProvider} from '../src/lib/useUpdater';
import {LocaleProvider} from '../src/lib/i18n';
import {SettingsPanel} from '../src/components/SettingsPanelEntry';
import type {SettingsTab} from '../src/lib/types';
import '../src/styles.css';
import '../src/components/SettingsPanel.css';
applyTheme('light', 'graphite');
createRoot(document.getElementById('root')!).render(<LocaleProvider><UpdaterProvider><SettingsPanel desktopPlatform="darwin" initialTab={(new URLSearchParams(location.search).get('page') || 'general') as SettingsTab} onClose={()=>{}} onChanged={()=>{}} onUseSubagent={()=>{}} /></UpdaterProvider></LocaleProvider>);
