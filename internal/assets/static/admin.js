// Admin helpers. Task 14 extends this file with photo cropping.
(function () {
  'use strict';

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
