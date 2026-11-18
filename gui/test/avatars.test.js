const { createHarness, createChecker } = require('./dom-harness');

(async () => {
  const { check, finish } = createChecker();

  const contacts = [
    {
      id: 'c-other',
      display_name: 'Contacto Sin Avatar',
      phone: '+5491199990000',
      avatar_url: '',
      channels: [{ id: 'ch-other', provider: 'whatsapp', remote_id: '5491199990000@s.whatsapp.net', handle: '' }]
    },
    {
      id: 'c-me-tg',
      display_name: 'Carlos',
      phone: '+5491122334455',
      avatar_url: '/tmp/avatars/tg_me.jpg',
      channels: [{ id: 'ch-me-tg', provider: 'telegram', remote_id: '100200300', handle: '@cestila' }]
    },
    {
      id: 'c-me-wa',
      display_name: 'Carlos WA',
      phone: '+5491122334455',
      avatar_url: 'https://cdn.whatsapp.net/wa_me.jpg',
      channels: [{ id: 'ch-me-wa', provider: 'whatsapp', remote_id: '5491122334455@s.whatsapp.net', handle: '' }]
    }
  ];

  const accounts = [
    { id: 'acc-tg', provider: 'telegram', label: 'Telegram Personal', identifier: '@cestila', status: 'connected' },
    { id: 'acc-wa', provider: 'whatsapp', label: 'Mi WhatsApp', identifier: '+5491122334455', status: 'connected' }
  ];

  const app = createHarness({ contacts, accounts });
  await app.start();

  // Render accounts grid / pills and check local-media normalization & self matching
  const pillsHtml = app.el.accountsPills.children.map(c => c.innerHTML).join('\n');

  check('convierte ruta local de avatar a local-media://', pillsHtml.includes('local-media://%2Ftmp%2Favatars%2Ftg_me.jpg'));
  check('resuelve avatar de cuenta WhatsApp por teléfono', pillsHtml.includes('https://cdn.whatsapp.net/wa_me.jpg'));

  finish();
})();
