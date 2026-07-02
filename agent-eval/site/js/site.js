document.addEventListener('DOMContentLoaded', () => {
  // Highlight current page in nav
  const path = location.pathname.split('/').pop() || 'index.html';
  document.querySelectorAll('.nav-links a').forEach(a => {
    const href = a.getAttribute('href').split('/').pop();
    if (href === path || (path === '' && href === 'index.html')) {
      a.classList.add('active');
    }
  });

  // Iframe loaders: hide placeholder once loaded
  document.querySelectorAll('.project-frame[data-lazy]').forEach(frame => {
    frame.addEventListener('load', () => {
      frame.classList.add('loaded');
    });
  });
});
