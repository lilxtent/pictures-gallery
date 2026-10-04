// Full-screen view of a painting. Without JS the link simply opens the image.
(function () {
  'use strict';
  var opener = document.querySelector('a[data-lightbox]');
  if (!opener) return;

  var box = document.createElement('div');
  box.className = 'lightbox';
  box.hidden = true;
  box.innerHTML = '<img alt=""><button type="button" class="lightbox-close" aria-label="Закрыть">×</button>';
  document.body.appendChild(box);
  var img = box.querySelector('img');

  function open(e) {
    e.preventDefault();
    img.src = opener.getAttribute('href');
    img.alt = opener.getAttribute('data-alt') || '';
    box.hidden = false;
    document.body.style.overflow = 'hidden';
  }

  function close() {
    box.hidden = true;
    document.body.style.overflow = '';
  }

  opener.addEventListener('click', open);
  box.addEventListener('click', close);
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !box.hidden) close();
  });
})();
