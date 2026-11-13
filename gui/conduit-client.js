const fs = require('fs');
const path = require('path');
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');

// The contract lives in the sdk module during development and is copied into
// the packaged app's resources by electron-builder (extraResources).
function resolveProtoPath() {
  const candidates = [
    process.env.CONDUIT_PROTO_PATH,
    path.resolve(__dirname, '../sdk/proto/conduit.proto'),
    process.resourcesPath && path.join(process.resourcesPath, 'proto', 'conduit.proto'),
  ].filter(Boolean);
  const found = candidates.find((candidate) => fs.existsSync(candidate));
  if (!found) {
    throw new Error(`conduit.proto not found; looked in: ${candidates.join(', ')}`);
  }
  return found;
}

class ConduitRpcClient {
  constructor(serverAddress = '127.0.0.1:50051') {
    this.serverAddress = serverAddress;
    this.client = null;
    this.eventStream = null;
    this.onEventCallback = null;
    this.init();
  }

  init() {
    const protoPath = resolveProtoPath();
    const packageDefinition = protoLoader.loadSync(protoPath, {
      keepCase: true,
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true
    });

    const protoDescriptor = grpc.loadPackageDefinition(packageDefinition);
    const conduitv1 = protoDescriptor.conduit.v1;

    this.client = new conduitv1.ConduitService(
      this.serverAddress,
      grpc.credentials.createInsecure()
    );
  }

  getStatus() {
    return new Promise((resolve, reject) => {
      this.client.GetStatus({}, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  listConversations(limit = 50, offset = 0) {
    return new Promise((resolve, reject) => {
      this.client.ListConversations({ limit, offset }, (err, response) => {
        if (err) return reject(err);
        resolve(response.conversations || []);
      });
    });
  }

  getMessages(conversationId, limit = 50, beforeMessageId = '') {
    return new Promise((resolve, reject) => {
      this.client.GetMessages({ conversation_id: conversationId, limit, before_message_id: beforeMessageId }, (err, response) => {
        if (err) return reject(err);
        resolve(response.messages || []);
      });
    });
  }

  getPinnedMessages(conversationId) {
    return new Promise((resolve, reject) => {
      this.client.GetPinnedMessages({ conversation_id: conversationId }, (err, response) => {
        if (err) return reject(err);
        resolve(response.messages || []);
      });
    });
  }

  sendMessage(conversationId, body, targetChannelId = '', replyToMessageId = '', attachments = []) {
    return new Promise((resolve, reject) => {
      this.client.SendMessage({
        conversation_id: conversationId,
        body,
        target_channel_id: targetChannelId,
        reply_to_message_id: replyToMessageId,
        attachments: (attachments || []).map(att => ({
          file_path: att.path || att.file_path || '',
          file_name: att.name || att.file_name || '',
          mime_type: att.mime || att.mime_type || '',
          file_size: Number(att.size ?? att.file_size) || 0
        }))
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response.message);
      });
    });
  }

  sendReaction(messageId, emoji = '') {
    return new Promise((resolve, reject) => {
      this.client.SendReaction({
        message_id: messageId,
        emoji: emoji || ''
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  sendVote(messageId, selectedOptions = []) {
    return new Promise((resolve, reject) => {
      this.client.SendVote({
        message_id: messageId,
        selected_options: selectedOptions || []
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  clickButton(messageId, buttonId, buttonText = '') {
    return new Promise((resolve, reject) => {
      this.client.ClickButton({
        message_id: messageId,
        button_id: buttonId,
        button_text: buttonText || ''
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  editMessage(messageId, newBody) {
    return new Promise((resolve, reject) => {
      this.client.EditMessage({
        message_id: messageId,
        new_body: newBody
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response.message);
      });
    });
  }

  deleteMessage(messageId) {
    return new Promise((resolve, reject) => {
      this.client.DeleteMessage({
        message_id: messageId
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  downloadAttachment(attachmentId, wait = false) {
    return new Promise((resolve, reject) => {
      this.client.DownloadAttachment({ attachment_id: attachmentId, wait }, (err, response) => {
        if (err) return reject(err);
        resolve(response.attachment);
      });
    });
  }

  pinMessage(messageId, pinned = true) {
    return new Promise((resolve, reject) => {
      this.client.PinMessage({
        message_id: messageId,
        pinned
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  listAccounts() {
    return new Promise((resolve, reject) => {
      this.client.ListAccounts({}, (err, response) => {
        if (err) return reject(err);
        resolve(response.accounts || []);
      });
    });
  }

  addAccount(provider, label, identifier, credentials = null) {
    return new Promise((resolve, reject) => {
      const request = { provider, label, identifier };
      if (credentials) request.credentials = credentials;
      this.client.AddAccount(request, (err, response) => {
        if (err) return reject(err);
        resolve(response.account);
      });
    });
  }

  removeAccount(accountId) {
    return new Promise((resolve, reject) => {
      this.client.RemoveAccount({ account_id: accountId }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  connectAccount(accountId) {
    return new Promise((resolve, reject) => {
      this.client.ConnectAccount({ account_id: accountId }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  disconnectAccount(accountId) {
    return new Promise((resolve, reject) => {
      this.client.DisconnectAccount({ account_id: accountId }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  listPlugins() {
    return new Promise((resolve, reject) => {
      this.client.ListPlugins({}, (err, response) => {
        if (err) return reject(err);
        resolve(response.plugins || []);
      });
    });
  }

  installPlugin(pluginId) {
    return new Promise((resolve, reject) => {
      this.client.InstallPlugin({ plugin_id: pluginId }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  togglePlugin(pluginId, enabled) {
    return new Promise((resolve, reject) => {
      this.client.TogglePlugin({ plugin_id: pluginId, enabled }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  searchMessages(query) {
    return new Promise((resolve, reject) => {
      this.client.SearchMessages({ query }, (err, response) => {
        if (err) return reject(err);
        resolve(response.messages || []);
      });
    });
  }

  getSettings() {
    return new Promise((resolve, reject) => {
      this.client.GetSettings({}, (err, response) => {
        if (err) return reject(err);
        resolve(response.settings || {});
      });
    });
  }

  updateSettings(settings) {
    return new Promise((resolve, reject) => {
      this.client.UpdateSettings({ settings }, (err, response) => {
        if (err) return reject(err);
        resolve(response.settings || {});
      });
    });
  }

  listContacts() {
    return new Promise((resolve, reject) => {
      this.client.ListContacts({}, (err, response) => {
        if (err) return reject(err);
        resolve(response.contacts || []);
      });
    });
  }

  mergeContacts(primaryContactId, secondaryContactId) {
    return new Promise((resolve, reject) => {
      this.client.MergeContacts({
        primary_contact_id: primaryContactId,
        secondary_contact_id: secondaryContactId
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response.merged_contact);
      });
    });
  }

  splitChannel(channelId, newContactName) {
    return new Promise((resolve, reject) => {
      this.client.SplitChannel({
        channel_id: channelId,
        new_contact_name: newContactName
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response.new_contact);
      });
    });
  }

  setContactIdentity(contactId, email, phone) {
    return new Promise((resolve, reject) => {
      this.client.SetContactIdentity({
        contact_id: contactId,
        email: email || '',
        phone: phone || ''
      }, (err, response) => {
        if (err) return reject(err);
        resolve(response.contact);
      });
    });
  }

  findContactsByIdentity(query) {
    return new Promise((resolve, reject) => {
      this.client.FindContactsByIdentity({ query }, (err, response) => {
        if (err) return reject(err);
        resolve(response.contacts || []);
      });
    });
  }

  markConversationRead(conversationId) {
    return new Promise((resolve, reject) => {
      this.client.MarkConversationRead({ conversation_id: conversationId }, (err, response) => {
        if (err) return reject(err);
        resolve(response);
      });
    });
  }

  startEventStream(onEvent, onError) {
    if (this.eventStream) {
      this.eventStream.cancel();
    }

    try {
      this.eventStream = this.client.StreamEvents({ event_types: [] });

      this.eventStream.on('data', (envelope) => {
        if (onEvent) onEvent(envelope);
      });

      this.eventStream.on('error', (err) => {
        if (err.code === grpc.status.CANCELLED) return;
        if (onError) onError(err);
      });

      this.eventStream.on('end', () => {
        // Stream finalizado
      });
    } catch (e) {
      if (onError) onError(e);
    }
  }

  stopEventStream() {
    if (this.eventStream) {
      this.eventStream.cancel();
      this.eventStream = null;
    }
  }
}

module.exports = ConduitRpcClient;
