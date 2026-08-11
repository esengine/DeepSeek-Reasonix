(function () {
  const versionSelect = document.querySelector('[data-release-version]');
  versionSelect?.addEventListener('change', () => {
    const version = String(versionSelect.value || '').replace(/^v/, '');
    if (/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
      window.location.href = `/changelog/v${version}/`;
    }
  });

  const links = Array.from(document.querySelectorAll('.release-toc a[href^="#"]'));
  const sections = links.map((link) => document.querySelector(link.getAttribute('href'))).filter(Boolean);
  if (!links.length || !sections.length) return;

  let ticking = false;
  const update = () => {
    ticking = false;
    let current = sections[0];
    for (const section of sections) {
      if (section.getBoundingClientRect().top <= 150) current = section;
    }
    links.forEach((link) => link.classList.toggle('active', link.hash === `#${current.id}`));
  };
  const queue = () => {
    if (ticking) return;
    ticking = true;
    requestAnimationFrame(update);
  };
  window.addEventListener('scroll', queue, { passive: true });
  window.addEventListener('resize', queue, { passive: true });
  update();
})();
