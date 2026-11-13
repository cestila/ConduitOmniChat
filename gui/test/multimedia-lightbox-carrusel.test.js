const { createHarness, createChecker } = require('./dom-harness');

async function testMultimediaLightboxAndCarrusel() {
  console.log('--- TEST MULTIMEDIA LIGHTBOX, CARRUSEL Y EMBEDS ---');
  const { check, finish } = createChecker();

  const conversations = [
    {
      id: 'conv-media',
      title: 'Chat Multimedia',
      unread_count: 0,
      last_message_at_unix: 1700000300,
    }
  ];

  const app = createHarness({ conversations });
  await app.start();

  const parseSocial = app.sandbox.parseSocialVideoLink;
  const buildCard = app.sandbox.renderSocialVideoCardHTML;

  // 1. Detección y parseo de enlaces de video social
  check('parseSocialVideoLink está definido', typeof parseSocial === 'function');

  const ytWatch = parseSocial('Miren este video https://www.youtube.com/watch?v=dQw4w9WgXcQ genial');
  check('detecta enlace YouTube watch?v=', ytMatchOk(ytWatch, 'dQw4w9WgXcQ'));

  const ytShort = parseSocial('Short: https://youtu.be/5O0vN8yK0vM');
  check('detecta enlace YouTube youtu.be', ytMatchOk(ytShort, '5O0vN8yK0vM'));

  const ytShortsDir = parseSocial('https://youtube.com/shorts/abc12345678?feature=share');
  check('detecta enlace YouTube /shorts/', ytMatchOk(ytShortsDir, 'abc12345678'));

  const ttVideo = parseSocial('Chequeen https://www.tiktok.com/@comedy/video/7123456789012345678');
  check('detecta enlace TikTok /video/', ttVideo && ttVideo.provider === 'tiktok' && ttVideo.id === '7123456789012345678');

  const igReel = parseSocial('Miren este reel https://www.instagram.com/reel/C8xyzABC123/?igsh=xyz');
  check('detecta enlace Instagram /reel/', igReel && igReel.provider === 'instagram' && igReel.id === 'C8xyzABC123');

  const igPost = parseSocial('Foto/video https://www.instagram.com/p/B_123456789/');
  check('detecta enlace Instagram /p/', igPost && igPost.provider === 'instagram' && igPost.id === 'B_123456789');

  const plainText = parseSocial('Hola a todos, cómo andan hoy?');
  check('texto sin video social devuelve null', plainText === null);

  // 2. Renderizado de tarjeta social en el chat
  const cardHTML = buildCard(ytWatch);
  check('tarjeta social contiene clase social-video-card', cardHTML.includes('social-video-card'));
  check('tarjeta social incluye badge de YouTube', cardHTML.includes('social-video-badge') && cardHTML.includes('YouTube'));
  check('tarjeta social tiene botón/overlay de play', cardHTML.includes('social-video-play-overlay'));
  check('tarjeta social invoca openSocialVideoLightbox', cardHTML.includes('openSocialVideoLightbox'));

  // 3. Renderizado de mensajes con adjuntos multimedia y enlaces
  app.state.activeConversationId = 'conv-media';
  app.state.messageCache.set('conv-media', {
    messages: [
      {
        id: 'msg-img-1',
        conversation_id: 'conv-media',
        sender_type: 'CONTACT',
        sent_at_unix: 1700000100,
        body: 'Una foto de las vacaciones',
        attachments: [
          {
            id: 'att-img-1',
            file_name: 'playa.jpg',
            mime_type: 'image/jpeg',
            local_path: '/tmp/conduit/playa.jpg',
            download_status: 'cached'
          }
        ]
      },
      {
        id: 'msg-vid-1',
        conversation_id: 'conv-media',
        sender_type: 'CONTACT',
        sent_at_unix: 1700000200,
        body: 'El gol de ayer',
        attachments: [
          {
            id: 'att-vid-1',
            file_name: 'gol.mp4',
            mime_type: 'video/mp4',
            local_path: '/tmp/conduit/gol.mp4',
            download_status: 'cached'
          }
        ]
      },
      {
        id: 'msg-yt-1',
        conversation_id: 'conv-media',
        sender_type: 'CONTACT',
        sent_at_unix: 1700000300,
        body: 'Escuchen este tema https://youtu.be/dQw4w9WgXcQ',
        attachments: []
      }
    ],
    lastFetch: Date.now()
  });

  const cached = app.state.messageCache.get('conv-media').messages;
  app.sandbox.renderMessages(cached);

  const containerHTML = app.el.messagesContainer.innerHTML;
  check('renderiza foto con onclick openMediaLightbox', containerHTML.includes("openMediaLightbox") && containerHTML.includes("playa.jpg"));
  check('renderiza video con botón Pantalla completa', containerHTML.includes("video-open-lightbox-btn") && containerHTML.includes("Pantalla completa"));
  check('renderiza tarjeta interactiva para el video de YouTube', containerHTML.includes("social-video-card"));

  // 4. Generación de Playlist para el Carrusel
  const playlist = app.sandbox.buildConversationMediaPlaylist();
  check('playlist contiene 3 elementos multimedia en orden', playlist.length === 3);
  check('elemento 1 es la imagen playa.jpg', playlist[0].type === 'image' && playlist[0].name === 'playa.jpg');
  check('elemento 2 es el video gol.mp4', playlist[1].type === 'video' && playlist[1].name === 'gol.mp4');
  check('elemento 3 es el embed de YouTube', playlist[2].type === 'embed' && playlist[2].embedUrl.includes('dQw4w9WgXcQ'));

  // 5. Apertura de Lightbox y navegación por Carrusel
  const modal = app.documentStub.getElementById('media-lightbox-modal');
  const content = app.documentStub.getElementById('lightbox-content');
  const title = app.documentStub.getElementById('lightbox-filename');
  const counter = app.documentStub.getElementById('lightbox-counter');
  const btnPrev = app.documentStub.getElementById('lightbox-btn-prev');
  const btnNext = app.documentStub.getElementById('lightbox-btn-next');

  // Abrir imagen
  app.sandbox.openMediaLightbox('/tmp/conduit/playa.jpg', 'image', 'playa.jpg', '/tmp/conduit/playa.jpg', 'att-img-1');
  check('modal lightbox visible tras abrir foto', !modal.classList.contains('hidden'));
  check('título muestra playa.jpg', title.textContent === 'playa.jpg');
  check('contador muestra 1 de 3', counter.textContent === '1 de 3');
  check('content contiene etiqueta img', content.innerHTML.includes('<img'));
  check('botones prev y next visibles', !btnPrev.classList.contains('hidden') && !btnNext.classList.contains('hidden'));

  // Avanzar al elemento 2 (video)
  app.sandbox.lightboxNext();
  check('después de next, título muestra gol.mp4', title.textContent === 'gol.mp4');
  check('contador muestra 2 de 3', counter.textContent === '2 de 3');
  check('content contiene etiqueta video con controls y autoplay', content.innerHTML.includes('<video') && content.innerHTML.includes('controls'));

  // Avanzar al elemento 3 (embed YouTube)
  app.sandbox.lightboxNext();
  check('después de next, contador muestra 3 de 3', counter.textContent === '3 de 3');
  check('content contiene etiqueta iframe con embed de YouTube', content.innerHTML.includes('<iframe') && content.innerHTML.includes('dQw4w9WgXcQ'));

  // Ciclo hacia adelante vuelve al elemento 1 (imagen)
  app.sandbox.lightboxNext();
  check('ciclo continuo vuelve a elemento 1 (1 de 3)', counter.textContent === '1 de 3' && content.innerHTML.includes('<img'));

  // Retroceder con lightboxPrev vuelve al elemento 3 (embed)
  app.sandbox.lightboxPrev();
  check('lightboxPrev retrocede al elemento 3 (3 de 3)', counter.textContent === '3 de 3' && content.innerHTML.includes('<iframe'));

  // 6. Navegación por teclado (Flechas y Escape)
  app.fireWindowEvent('keydown', { key: 'ArrowLeft' });
  check('ArrowLeft retrocede a 2 de 3 (video)', counter.textContent === '2 de 3' && content.innerHTML.includes('<video'));

  app.fireWindowEvent('keydown', { key: 'ArrowRight' });
  check('ArrowRight avanza a 3 de 3 (embed)', counter.textContent === '3 de 3' && content.innerHTML.includes('<iframe'));

  app.fireWindowEvent('keydown', { key: 'Escape' });
  check('Escape cierra el modal lightbox', modal.classList.contains('hidden'));
  check('cerrar lightbox limpia el contenido para detener reproducción', content.innerHTML === '');

  // 7. Apertura de video social directo en lightbox
  app.sandbox.openSocialVideoLightbox('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1', 'YouTube: Video', 'https://youtu.be/dQw4w9WgXcQ');
  check('openSocialVideoLightbox abre modal', !modal.classList.contains('hidden'));
  check('openSocialVideoLightbox muestra iframe', content.innerHTML.includes('<iframe'));
  check('openSocialVideoLightbox posiciona carrusel en 3 de 3', counter.textContent === '3 de 3');

  app.sandbox.closeMediaLightbox();
  check('closeMediaLightbox oculta modal', modal.classList.contains('hidden'));

  finish();
}

function ytMatchOk(match, expectedId) {
  return match && match.provider === 'youtube' && match.id === expectedId;
}

testMultimediaLightboxAndCarrusel().catch(e => {
  console.error('FAIL testMultimediaLightboxAndCarrusel:', e);
  process.exit(1);
});
