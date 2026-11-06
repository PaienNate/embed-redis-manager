<template>
  <el-menu
    ref="connectionMenu"
    :collapse-transition='false'
    :id="connectionAnchor"
    @open="openConnection()"
    class="connection-menu"
    active-text-color="#ffd04b">
    <el-submenu :index="config.connectionName">
      <!-- connection menu -->
      <ConnectionMenu
        slot="title"
        :config="config"
        :client='client'
        @changeColor='setColor'
        @refreshConnection='openConnection(false, true)'>
      </ConnectionMenu>

      <!-- db search operate -->
      <OperateItem
        ref='operateItem'
        :config="config"
        :client='client'>
      </OperateItem>

      <!-- key list -->
      <KeyList
        ref='keyList'
        :config="config"
        :globalSettings='globalSettings'
        :client='client'>
      </KeyList>
    </el-submenu>
  </el-menu>
</template>

<script type="text/javascript">
import redisClient from '@/redisClient.js';
import KeyList from '@/components/KeyList';
import OperateItem from '@/components/OperateItem';
import ConnectionMenu from '@/components/ConnectionMenu';

export default {
  data() {
    return {
      client: null,
      pingTimer: null,
      pingInterval: 10000, // ms
      lastSelectedDb: 0,
    };
  },
  props: ['config', 'globalSettings', 'index'],
  components: { ConnectionMenu, OperateItem, KeyList },
  created() {
    this.$bus.$on('closeConnection', (connectionName = false) => {
      this.closeConnection(connectionName);
    });
    // open connection
    this.$bus.$on('openConnection', (connectionName) => {
      if (connectionName && (connectionName == this.config.connectionName)) {
        this.openConnection();
        this.$refs.connectionMenu.open(this.config.connectionName);
      }
    });
  },
  computed: {
    connectionAnchor() {
      return `connection-anchor-${this.config.connectionName}`;
    },
  },
  methods: {
    initShow() {
      // The connection's client resolves synchronously (HttpRedisClient is
      // always "ready"), so this can run before the submenu's children
      // (OperateItem/KeyList) are mounted and their $refs exist. Defer to the
      // next DOM update and guard the refs; otherwise the first open throws and
      // the initial key scan never happens (key list stays "No Data").
      this.$nextTick(() => {
        this.$refs.operateItem && this.$refs.operateItem.initShow();
        this.$refs.keyList && this.$refs.keyList.initShow();
      });
    },
    initLastSelectedDb() {
      const db = parseInt(localStorage.getItem(`lastSelectedDb_${this.config.connectionName}`));

      if (db > 0 && this.lastSelectedDb != db) {
        this.lastSelectedDb = db;
        this.$refs.operateItem && this.$refs.operateItem.setDb(db);
      }
    },
    openConnection(callback = false, forceOpen = false) {
      // scroll to connection
      this.scrollToConnection();
      // recovery last selected db
      this.initLastSelectedDb();

      // opened, do nothing
      if (this.client) {
        return forceOpen ? this.afterOpenConnection(this.client, callback) : false;
      }

      // set searching status first
      this.$refs.operateItem.searchIcon = 'el-icon-loading';

      // create a new client
      const clientPromise = this.getRedisClient(this.config);

      clientPromise.then((realClient) => {
        this.afterOpenConnection(realClient, callback);
      }).catch((e) => {});
    },
    afterOpenConnection(client, callback = false) {
      // new connection, not ready
      if (client.status != 'ready') {
        client.on('ready', () => {
          if (client.readyInited) {
            return;
          }

          client.readyInited = true;
          // open status tab
          this.$bus.$emit('openStatus', client, this.config.connectionName);
          this.startPingInterval();

          this.initShow();
          callback && callback();
        });
      }

      // connection is ready
      else {
        // HttpRedisClient.status is always 'ready' (no on('ready') event), so
        // open the status tab and start the heartbeat ping right here, matching
        // the behavior of the not-ready/on('ready') branch above.
        this.$bus.$emit('openStatus', client, this.config.connectionName);
        this.startPingInterval();
        this.initShow();
        callback && callback();
      }
    },
    closeConnection(connectionName) {
      // if connectionName is not passed, close all connections
      if (connectionName && (connectionName != this.config.connectionName)) {
        return;
      }

      this.$refs.connectionMenu
      && this.$refs.connectionMenu.close(this.config.connectionName);
      this.$bus.$emit('removeAllTab', connectionName);

      // clear ping interval
      clearInterval(this.pingTimer);

      // reset operateItem items
      this.$refs.operateItem && this.$refs.operateItem.resetStatus();
      // reset keyList items
      this.$refs.keyList && this.$refs.keyList.resetKeyList(true);

      this.client && this.client.quit && this.client.quit();
      this.client = null;
    },
    startPingInterval() {
      this.pingTimer = setInterval(() => {
        this.client && this.client.ping().then((reply) => {}).catch((e) => {
          // this.$message.error('Ping Error: ' + e.message);
        });
      }, this.pingInterval);
    },
    getRedisClient(config) {
      // prevent changing back to raw config, such as config.db
      const configCopy = JSON.parse(JSON.stringify(config));
      // select db
      configCopy.db = this.lastSelectedDb;

      // Embedded build: connections are pre-established by the backend and
      // identified by the backend-provided `id` (the connID), so we create an
      // HttpRedisClient proxy bound to it instead of a direct redis socket.
      const clientPromise = redisClient.createConnection(configCopy.id, {
        connectionName: this.config.connectionName,
        db: this.lastSelectedDb,
      });

      clientPromise.then((client) => {
        this.client = client;

        client.on('error', (error) => {
          this.$message.error({
            message: `Client On Error: ${error} Config right?`,
            duration: 3000,
            customClass: 'redis-on-error-message',
          });

          this.$bus.$emit('closeConnection');
        });
      }).catch((error) => {
        this.$message.error(error.message);
        this.$bus.$emit('closeConnection');
      });

      return clientPromise;
    },
    setColor(color, save = true) {
      const ulDom = this.$refs.connectionMenu.$el;
      const className = 'menu-with-custom-color';

      // Persist per connection id in localStorage. The embedded build's
      // connections are injected by the host (not stored locally), so
      // editConnectionItem() has nothing to write to.
      if (save && this.config.id) {
        try {
          if (color) {
            localStorage.setItem(`redisui:color:${this.config.id}`, color);
          } else {
            localStorage.removeItem(`redisui:color:${this.config.id}`);
          }
        } catch (e) { /* ignore quota/security errors */ }
      }

      if (!color) {
        ulDom.classList.remove(className);
      } else {
        ulDom.classList.add(className);
        this.$el.style.setProperty('--menu-color', color);
      }
    },
    scrollToConnection() {
      this.$nextTick(() => {
        // 300ms after menu expand animination
        setTimeout(() => {
          const menu = this.$refs.connectionMenu.$el;
          menu.scrollIntoView({ behavior: 'smooth' })
        }, 320);
      });
    },
  },
  mounted() {
    let color = this.config.color;
    if (!color && this.config.id) {
      try {
        color = localStorage.getItem(`redisui:color:${this.config.id}`) || '';
      } catch (e) {
        color = '';
      }
    }
    this.setColor(color, false);
  },
  beforeDestroy() {
    this.closeConnection(this.config.connectionName);
  },
};
</script>

<style type="text/css">
  /*menu ul*/
  .connection-menu {
    margin-bottom: 8px;
    padding-right: 6px;
    border-right: 0;
  }

  .connection-menu.menu-with-custom-color li.el-submenu {
    border-left: 5px solid var(--menu-color);
    border-radius: 4px 0 0 4px;
    padding-left: 3px;
  }

  /*this error shows first*/
  .redis-on-error-message {
    z-index:9999 !important;
  }
</style>
