"""Run against an isolated mock Vite server; never invokes a model provider."""
import argparse
import json
from playwright.sync_api import sync_playwright, expect

parser = argparse.ArgumentParser()
parser.add_argument('--url', default='http://127.0.0.1:5189/?mock=bench&bench=1')
parser.add_argument('--chromium', required=True)
args = parser.parse_args()

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True, executable_path=args.chromium)
    page = browser.new_page(viewport={'width': 1440, 'height': 1000})
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))
    page.goto(args.url)
    page.wait_for_load_state('networkidle')
    composer = page.locator('textarea.composer__input:not([aria-hidden=true])')
    project_new = page.locator('.project-tree__folder-action--create').first
    history = page.locator('.project-tree__topic-main').filter(has_text='bench:small-6t').first
    history.click()
    expect(composer).to_be_enabled()
    rows = page.locator('.project-tree__topic-main').count()
    page.locator('.project-tree__folder--project').first.hover()
    project_new.click()
    expect(page.locator('.main--draft-landing')).to_have_count(1)
    expect(page.locator('.welcome-creation__headline')).to_be_visible()
    expect(page.locator('.session-draft-surface')).to_have_count(0)
    composer.fill('persistent draft ownership fixture')
    for _ in range(20):
        page.locator('.project-tree__folder--project').first.hover()
        project_new.click()
    expect(composer).to_have_value('persistent draft ownership fixture')
    assert page.locator('.project-tree__topic-main').count() == rows
    page.locator('.sidebar__quick-action').first.click()
    expect(composer).to_have_value('persistent draft ownership fixture')
    history.click()
    expect(page.locator('.main--draft-landing')).to_have_count(0)
    page.keyboard.press('Meta+n')
    expect(page.locator('.main--draft-landing')).to_have_count(1)
    expect(page.locator('.welcome-creation__headline')).to_be_visible()
    expect(composer).to_have_value('persistent draft ownership fixture')
    history.click()
    expect(page.locator('.main--draft-landing')).to_have_count(0)
    page.locator('.project-tree__folder--project').first.click(button='right')
    page.get_by_role('menuitem', name='New session', exact=True).click()
    expect(composer).to_have_value('persistent draft ownership fixture')
    history.click()
    expect(page.locator('.main--draft-landing')).to_have_count(0)
    page.keyboard.press('Meta+k')
    page.locator('.palette input').fill('New session')
    page.get_by_role('option').filter(has_text='New session').first.click()
    expect(composer).to_have_value('persistent draft ownership fixture')
    page.evaluate('() => window.__reasonixFlushSessionDraft()')
    page.evaluate('() => window.__reasonixResumeSessionDraftEditing()')
    # Each sample begins from a different, fully installed history surface.
    times = []
    for _ in range(30):
        history.click()
        expect(page.locator('.main--draft-landing')).to_have_count(0)
        expect(page.locator('.transcript-navigation-surface[aria-busy=false]')).to_be_visible()
        elapsed = page.evaluate('''() => new Promise(resolve => {
          const start = performance.now();
          document.querySelector('.project-tree__folder-action--create').click();
          const inspect = () => {
            const input = document.querySelector('textarea.composer__input:not([aria-hidden=true])');
            if (document.querySelector('.main--draft-landing') && input && !input.disabled && input.value === 'persistent draft ownership fixture') resolve(performance.now()-start);
            else requestAnimationFrame(inspect);
          };
          requestAnimationFrame(inspect);
        })''')
        times.append(elapsed)
    expect(composer).to_have_value('persistent draft ownership fixture')
    assert not errors, errors
    print(json.dumps({'mode': 'mock-browser', 'new_clicks': 20, 'entrypoints': ['project-plus','top-new','shortcut','context-menu','command-palette'], 'session_rows_unchanged': True,
                      'restore_samples': len(times), 'restore_p95_ms': sorted(times)[28], 'errors': errors}))
    browser.close()
