// Admin helpers: photo cropping, unsaved-changes warning, drag-to-reorder.
(function () {
  'use strict';
  var MAX_BYTES = 30 * 1024 * 1024;

  // Photo cropping with Cropper.js. The browser only measures the crop; the
  // server cuts the stored original, so nothing is lost by cropping.
  document.querySelectorAll('[data-crop]').forEach(function (box) {
    var form = box.closest('form');
    var input = box.querySelector('input[type=file]');
    var stage = box.querySelector('.crop-stage');
    var img = stage.querySelector('img');
    var tools = box.querySelector('.crop-tools');
    var hint = box.querySelector('.crop-hint');
    var current = box.querySelector('.photo-current');
    var recrop = box.querySelector('[data-recrop]');
    var cropper = null;

    function field(name) { return form.querySelector('input[name="' + name + '"]'); }

    function start(src, initial) {
      if (cropper) { cropper.destroy(); cropper = null; }
      stage.hidden = false;
      tools.hidden = false;
      hint.hidden = false;
      if (current) current.hidden = true;
      img.onload = function () {
        img.onload = null;
        cropper = new Cropper(img, {
          viewMode: 1,
          autoCropArea: 1,
          checkOrientation: false, // the browser already applies EXIF orientation, like the server
          background: false,
          zoomable: false,
          data: initial || undefined
        });
      };
      img.src = src;
      field('crop_changed').value = '1';
      form.dispatchEvent(new Event('change'));
    }

    input.addEventListener('change', function () {
      var file = input.files && input.files[0];
      if (!file) return;
      if (file.size > MAX_BYTES) {
        alert('Фото слишком большое (максимум 30 МБ)');
        input.value = '';
        return;
      }
      start(URL.createObjectURL(file));
    });

    if (recrop) {
      recrop.addEventListener('click', function () {
        var initial = recrop.getAttribute('data-crop');
        start(recrop.getAttribute('data-original'), initial ? JSON.parse(initial) : null);
      });
    }

    tools.querySelectorAll('[data-rotate]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        if (cropper) cropper.rotate(Number(btn.getAttribute('data-rotate')));
      });
    });

    form.addEventListener('submit', function (e) {
      if (box.hasAttribute('data-required') && !(input.files && input.files.length)) {
        e.preventDefault();
        alert('Выберите фото картины');
        return;
      }
      if (!cropper) return;
      var d = cropper.getData(true);
      field('crop_x').value = d.x;
      field('crop_y').value = d.y;
      field('crop_w').value = d.width;
      field('crop_h').value = d.height;
      field('crop_rotate').value = d.rotate || 0;
    });
  });

  // Warn before leaving a form with unsaved changes.
  document.querySelectorAll('form[data-dirty-check]').forEach(function (form) {
    var dirty = false;
    form.addEventListener('input', function () { dirty = true; });
    form.addEventListener('change', function () { dirty = true; });
    form.addEventListener('submit', function (e) { if (!e.defaultPrevented) dirty = false; });
    window.addEventListener('beforeunload', function (e) {
      if (dirty) { e.preventDefault(); e.returnValue = ''; }
    });
  });

  // Drag-to-reorder on the paintings list.
  var list = document.querySelector('[data-sortable]');
  if (list && window.Sortable) {
    var status = document.querySelector('[data-sort-status]');
    Sortable.create(list, {
      handle: '.drag',
      animation: 150,
      onEnd: function () {
        var ids = Array.prototype.map.call(list.querySelectorAll('[data-id]'), function (li) {
          return Number(li.getAttribute('data-id'));
        });
        status.textContent = 'Сохраняю…';
        fetch('/admin/paintings/reorder', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': list.getAttribute('data-csrf') },
          body: JSON.stringify({ ids: ids })
        }).then(function (r) {
          status.textContent = r.ok ? 'Порядок сохранён' : 'Не удалось сохранить порядок. Обновите страницу.';
        }).catch(function () {
          status.textContent = 'Не удалось сохранить порядок. Проверьте интернет.';
        });
      }
    });
  }
})();
