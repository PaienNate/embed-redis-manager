<template>
  <div class="aside-outer-container">
    <div class="aside-header">
      <div class="aside-top-container">
        <!-- connection / page status -->
        <span class="conn-status" :class="connStatus" :title="statusText">
          <i class="conn-status-dot"></i>{{ statusText }}
        </span>
        <div class="aside-top-actions">
          <el-button class='aside-setting-btn' type="primary" icon="el-icon-time" @click="$refs.commandLogDialog.show()" :title='$t("message.command_log")+" Ctrl+g"' plain></el-button>
          <el-button class='aside-setting-btn' type="primary" icon="el-icon-setting" @click="$refs.settingDialog.show()" :title='$t("message.settings")+" Ctrl+,"' plain></el-button>
        </div>
      </div>

      <!-- user settings -->
      <Setting ref="settingDialog"></Setting>

      <!-- redis command logs -->
      <CommandLog ref='commandLogDialog'></CommandLog>
      <!-- hot key tips dialog -->
      <HotKeys ref='hotKeysDialog'></HotKeys>
      <!-- custom shell formatter -->
      <CustomFormatter></CustomFormatter>
    </div>

    <!-- connection list -->
    <Connections ref="connections"></Connections>
  </div>
</template>

<script type="text/javascript">
import Setting from '@/components/Setting';
import Connections from '@/components/Connections';
import CommandLog from '@/components/CommandLog';
import HotKeys from '@/components/HotKeys';
import CustomFormatter from '@/components/CustomFormatter';

export default {
  data() {
    return {
      connStatus: 'checking', // checking | connected | disconnected
      statusConnID: '',
      statusTimer: null,
    };
  },
  components: {
    Connections, Setting, CommandLog, HotKeys, CustomFormatter,
  },
  computed: {
    statusText() {
      if (this.connStatus === 'connected') {
        return this.$t('message.status_connected');
      }
      if (this.connStatus === 'disconnected') {
        return this.$t('message.status_disconnected');
      }
      return this.$t('message.status_checking');
    },
  },
  methods: {
    _apiBase() {
      const base = (typeof window !== 'undefined' && window.__REDISUI__ && window.__REDISUI__.apiBase) || '/api';
      return String(base).replace(/\/+$/, '');
    },
    async _pickConnID() {
      try {
        const resp = await fetch(`${this._apiBase()}/connections`);
        if (!resp.ok) {
          return null;
        }
        const list = await resp.json();
        const conn = (list || []).find((c) => c.enabled !== false);
        return conn ? conn.id : null;
      } catch (e) {
        return null;
      }
    },
    async checkStatus() {
      const id = this.statusConnID || await this._pickConnID();
      if (!id) {
        this.connStatus = 'disconnected';
        return;
      }
      this.statusConnID = id;
      try {
        const resp = await fetch(`${this._apiBase()}/redis/${id}/command`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ cmd: 'ping', args: [] }),
        });
        const data = await resp.json();
        this.connStatus = (resp.ok && !data.error) ? 'connected' : 'disconnected';
      } catch (e) {
        this.connStatus = 'disconnected';
        this.statusConnID = ''; // re-resolve the connection next round
      }
    },
    startStatusPolling() {
      this.checkStatus();
      this.statusTimer = setInterval(() => this.checkStatus(), 10000);
    },
    initShortcut() {
      // settings
      this.$shortcut.bind('ctrl+,', () => {
        this.$refs.settingDialog.show();
        return false;
      });
      this.$shortcut.bind('⌘+,', () => {
        this.$refs.settingDialog.show();
        return false;
      });
      // logs
      this.$shortcut.bind('ctrl+g, ⌘+g', () => {
        this.$refs.commandLogDialog.show();
        return false;
      });
    },
  },
  mounted() {
    this.initShortcut();
    this.startStatusPolling();
  },
  beforeDestroy() {
    clearInterval(this.statusTimer);
  },
};
</script>

<style type="text/css">
  /* Whole aside as a flex column so the connection list fills the remaining
     height without fixed 100vh calcs or float/overflow paint artifacts. */
  .aside-outer-container {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }
  .aside-header {
    flex: 0 0 auto;
  }
  .aside-top-container {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 33px;
    margin: 0 8px;
  }
  .aside-top-actions {
    display: flex;
    align-items: center;
  }
  .aside-top-container .aside-setting-btn {
    width: 44px;
    margin-left: 5px;
    margin-right: 0;
  }

  .conn-status {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    line-height: 1;
    color: #909399;
    white-space: nowrap;
    user-select: none;
  }
  .conn-status .conn-status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #c0c4cc;
    flex: 0 0 auto;
  }
  .conn-status.connected {
    color: #67c23a;
  }
  .conn-status.connected .conn-status-dot {
    background: #67c23a;
  }
  .conn-status.disconnected {
    color: #f56c6c;
  }
  .conn-status.disconnected .conn-status-dot {
    background: #f56c6c;
  }
  .conn-status.checking .conn-status-dot {
    background: #e6a23c;
  }

  .dark-mode .aside-top-container .el-button--info {
    color: #52a6fd;
    background: inherit;
  }
</style>
