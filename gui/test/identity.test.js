// Prueba estructural de la identidad canónica en la vista de Contactos:
// email/teléfono visibles, edición manual y vinculación por identidad.
// Ejecutar con: node test/identity.test.js   (o npm run test:identity)

const { createHarness, createChecker } = require('./dom-harness');

const contacts = [
  {
    id: 'contact-martin',
    display_name: 'Martín Gómez',
    email: 'martin@example.com',
    phone: '+5491112345678',
    channels: [
      { id: 'ch-wa', provider: 'PROVIDER_WHATSAPP', remote_id: '+5491112345678', handle: 'Martín' },
      { id: 'ch-tg', provider: 'PROVIDER_TELEGRAM', remote_id: 'tg-9981', handle: '@martingomez' },
    ],
  },
  {
    id: 'contact-sin-identidad',
    display_name: 'Discord Anónimo',
    channels: [
      { id: 'ch-dc', provider: 'PROVIDER_DISCORD', remote_id: 'discord-user-999', handle: 'anon#1234' },
    ],
  },
];

const identityResults = [
  {
    id: 'contact-sin-identidad',
    display_name: 'Discord Anónimo',
    email: 'martin@example.com',
    phone: '',
    channels: [{ id: 'ch-dc', provider: 'PROVIDER_DISCORD', remote_id: 'discord-user-999', handle: 'anon#1234' }],
  },
];

(async () => {
  const app = createHarness({ contacts, identityResults });
  await app.start();

  const { check, finish } = createChecker();
  const el = app.el;

  // 1. La tarjeta de contacto muestra la identidad canónica persistida
  const cards = el.contactsGrid.children;
  check('se renderizan 2 contactos', cards.length === 2, `obtuvo ${cards.length}`);
  const martinCard = cards.find(card => (card.innerHTML || '').includes('Martín Gómez'));
  check('la tarjeta muestra el email canónico', !!martinCard && martinCard.innerHTML.includes('martin@example.com'));
  check('la tarjeta muestra el teléfono canónico', !!martinCard && martinCard.innerHTML.includes('+5491112345678'));
  check('la tarjeta ofrece definir identidad', !!martinCard && martinCard.innerHTML.includes('editContactIdentity'));

  const anonymousCard = cards.find(card => (card.innerHTML || '').includes('Discord Anónimo'));
  check('un contacto sin identidad lo indica explícitamente', !!anonymousCard && anonymousCard.innerHTML.includes('Sin identidad canónica'));

  // 2. Los selectores de identidad se pueblan con el nombre y la identidad
  app.populateIdentitySelects();
  const options = el.identityContactSelect.children;
  check('el selector de identidad lista todos los contactos', options.length === 2, `obtuvo ${options.length}`);
  check('la opción incluye email o teléfono', options.some(o => o.textContent.includes('martin@example.com')));

  // 3. Búsqueda por identidad: ofrece fusionar el contacto encontrado
  el.identityTargetSelect.value = 'contact-martin';
  el.identityQueryInput.value = 'martin@example.com';
  await app.searchContactsByIdentity();

  const resultsHTML = el.identityResults.innerHTML;
  check('la búsqueda por email encuentra el contacto', resultsHTML.includes('Discord Anónimo'), resultsHTML.slice(0, 120));
  check('la búsqueda ofrece fusionar en el contacto principal', resultsHTML.includes('mergeIdentityMatch'));
  check('la búsqueda muestra la identidad del contacto encontrado', resultsHTML.includes('martin@example.com'));

  // 4. Fusionar el resultado usa el contacto principal elegido como destino
  await app.sandbox.mergeIdentityMatch('contact-sin-identidad');
  check('la fusión usa el contacto principal seleccionado', app.calls.merge.length === 1 && app.calls.merge[0][0] === 'contact-martin', JSON.stringify(app.calls.merge));
  check('la fusión envía el contacto encontrado como secundario', app.calls.merge[0][1] === 'contact-sin-identidad', JSON.stringify(app.calls.merge));

  // 5. Un contacto que ya es el principal no se ofrece fusionar consigo mismo
  el.identityTargetSelect.value = 'contact-martin';
  el.identityQueryInput.value = 'martin@example.com';
  app.sandbox.__state = app.state;
  const selfMatch = [{ id: 'contact-martin', display_name: 'Martín Gómez', email: 'martin@example.com', channels: [] }];
  app.sandbox.window.conduitAPI.findContactsByIdentity = async () => selfMatch;
  await app.searchContactsByIdentity();
  check('el contacto principal no se ofrece fusionar consigo mismo', el.identityResults.innerHTML.includes('Ya es el contacto principal'));

  // 6. Guardar identidad envía email y teléfono al core
  el.identityContactSelect.value = 'contact-sin-identidad';
  el.identityEmailInput.value = 'nuevo@example.com';
  el.identityPhoneInput.value = '+5491100003333';
  await app.sandbox.window.conduitAPI.setContactIdentity(
    el.identityContactSelect.value,
    el.identityEmailInput.value,
    el.identityPhoneInput.value
  );
  check('setContactIdentity envía la identidad del contacto elegido', app.calls.identity.length === 1 && app.calls.identity[0][0] === 'contact-sin-identidad', JSON.stringify(app.calls.identity));

  finish();
})();
