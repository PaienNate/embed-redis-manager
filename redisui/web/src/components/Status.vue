<template>
<div>
  <!-- auto refresh row -->
  <el-row>
    <el-col>
      <div style="float: right;">
        <el-tag type="info">
          <i class="el-icon-refresh"></i>
          {{ $t('message.auto_refresh') }}
        </el-tag>

        <el-tooltip class="item" effect="dark" :content="$t('message.auto_refresh_tip', {interval: refreshInterval / 1000})" placement="bottom">
          <el-switch v-model="autoRefresh" @change="refreshInit">
          </el-switch>
        </el-tooltip>
      </div>
    </el-col>
  </el-row>

  <!-- server status row -->
  <el-row :gutter="10" class="status-container status-card">
    <!-- server -->
    <el-col :span="8">
      <el-card class="box-card">
        <div slot="header">
          <i class="fa fa-server"></i>
          <span>{{ $t('message.server') }}</span>
        </div>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.redis_version') }}:
            <span class="server-status-text">{{this.connectionStatus.redis_version}}</span>
          </el-tag>
        </p>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            OS:
            <span class="server-status-text" :title="connectionStatus.os">{{this.connectionStatus.os}}</span>
          </el-tag>
        </p>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.process_id') }}:
            <span class="server-status-text">{{this.connectionStatus.process_id}}</span>
          </el-tag>
        </p>
      </el-card>
    </el-col>

    <!-- memory row -->
    <el-col :span="8">
      <el-card class="box-card">
        <div slot="header">
          <i class="fa fa-microchip"></i>
          <span>{{ $t('message.memory') }}</span>
        </div>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.used_memory') }}:
            <span class="server-status-text">{{$util.humanFileSize(connectionStatus.used_memory)}}</span>
          </el-tag>
        </p>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.used_memory_peak') }}:
            <span class="server-status-text">{{$util.humanFileSize(connectionStatus.used_memory_peak)}}</span>
          </el-tag>
        </p>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.used_memory_lua') }}:
            <span class="server-status-text">{{$util.humanFileSize(connectionStatus.used_memory_lua)}}</span>
          </el-tag>
        </p>
      </el-card>
    </el-col>

    <!-- stats row -->
    <el-col :span="8">
      <el-card class="box-card">
        <div slot="header">
          <i class="fa fa-thermometer-three-quarters"></i>
          <span>{{ $t('message.stats') }}</span>
        </div>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.connected_clients') }}:
            <span class="server-status-text">{{this.connectionStatus.connected_clients}}</span>
          </el-tag>
        </p>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.total_connections_received') }}:
            <span class="server-status-text">{{this.connectionStatus.total_connections_received}}</span>
          </el-tag>
        </p>

        <p class="server-status-tag-p">
          <el-tag class='server-status-container' type="info" size="big">
            {{ $t('message.total_commands_processed') }}:
            <span class="server-status-text">{{this.connectionStatus.total_commands_processed}}</span>
          </el-tag>
        </p>
      </el-card>
    </el-col>
  </el-row>

  <!-- client options -->
  <el-row v-if="connectionOptions.length" class="status-card">
    <el-col>
      <el-card class="box-card">
        <div slot="header">
          <i class="fa fa-sliders"></i>
          <span>{{ $t('message.connection_options') }}</span>
        </div>

        <el-table
          :data="connectionOptions"
          size="mini"
          border
          max-height="420"
          style="width: 100%">
          <el-table-column
            prop="key"
            label="Key"
            width="240">
          </el-table-column>
          <el-table-column
            prop="value"
            label="Value"
            show-overflow-tooltip>
          </el-table-column>
        </el-table>
      </el-card>
    </el-col>
  </el-row>

  <!-- cluster key statistics -->
  <el-row class="status-card">
    <el-col>
      <el-card class="box-card">
        <div slot="header">
          <i class="fa fa-bar-chart"></i>
          <span>{{ $t('message.key_statistics') }}</span>
        </div>

        <el-table
          :data="DBKeys"
          stripe>
          <el-table-column
            v-if="isCluster"
            prop="name"
            sortable
            label="Node">
          </el-table-column>
          <el-table-column
            prop="db"
            sortable
            label="DB">
          </el-table-column>
          <el-table-column
            sortable
            prop="keys_show"
            label="Keys"
            :sort-method="sortByKeys">
          </el-table-column>
          <el-table-column
            sortable
            prop="expires_show"
            label="Expires"
            :sort-method="sortByExpires">
          </el-table-column>
          <!-- avg_ttl: tooltip can't be removed!, or the table's height will change -->
          <el-table-column
            sortable
            prop="avg_ttl_show"
            :show-overflow-tooltip='true'
            label="Avg TTL"
            :sort-method="sortByTTL">
          </el-table-column>
        </el-table>
      </el-card>
    </el-col>
  </el-row>

  <!-- redis all info -->
  <el-row class="status-card">
    <el-col>
      <el-card class="box-card">
        <div slot="header">
          <i class="fa fa-info-circle"></i>
          <span>{{ $t('message.all_redis_info') }}</span>
          <!-- search input -->
          <el-input v-model='allInfoFilter' size='mini' suffix-icon="el-icon-search" class='status-filter-input'>
          </el-input>
        </div>

        <el-table
          :data="AllRedisInfo"
          stripe>
          <el-table-column
            prop="key"
            sortable
            label="Key">
          </el-table-column>
          <el-table-column
            prop="value"
            :show-overflow-tooltip='true'
            label="Value">
          </el-table-column>
        </el-table>
      </el-card>
    </el-col>
  </el-row>

  <ScrollToTop parentNum='1'></ScrollToTop>
</div>
</template>

<script>
import ScrollToTop from '@/components/ScrollToTop';

export default {
  data() {
    return {
      autoRefresh: false,
      refreshTimer: null,
      refreshInterval: 2000,
      connectionStatus: {},
      allInfoFilter: '',
      DBKeys: [],
      connectionOptions: [],
      connectionOptionsFor: null,
    };
  },
  props: ['client', 'hotKeyScope'],
  components: { ScrollToTop },
  computed: {
    AllRedisInfo() {
      const infos = [];
      const filter = this.allInfoFilter.toLowerCase();

      // filter mode
      if (filter) {
        for (const i in this.connectionStatus) {
          if (i.includes(filter)) {
            infos.push({ key: i, value: this.connectionStatus[i] });
          }
        }
      }
      // all info
      else {
        for (const i in this.connectionStatus) {
          infos.push({ key: i, value: this.connectionStatus[i] });
        }
      }

      return infos;
    },
    isCluster() {
      return this.connectionStatus['cluster_enabled'] == '1';
    },
  },
  methods: {
    initShow() {
      // set global info dict
      this.client.ardmInfo = {};
      this.fetchConnectionOptions();

      this.client.info().then((reply) => {
        this.connectionStatus = this.initStatus(reply);
        this.client.ardmInfo = this.connectionStatus;

        // init db keys info
        if (this.isCluster) {
          this.initClusterKeys();
        }
        else {
          this.DBKeys = this.initDbKeys(this.connectionStatus);
        }
      }).catch((e) => {
        // info command may be disabled
        if (e.message.includes('unknown command')) {
          this.$message.error({
            message: this.$t('message.info_disabled'),
            duration: 3000,
          });
        }
        // no auth not show
        else if (e.message.includes('NOAUTH')) {} else {
          this.$message.error(e.message);
        }
      });
    },
    fetchConnectionOptions() {
      const client = this.client;
      if (!client || typeof client.connectionOptions !== 'function') {
        return;
      }
      // Options are fixed at injection time; fetch once per connection even
      // when the auto-refresh timer keeps calling initShow().
      if (this.connectionOptionsFor === client.connID) {
        return;
      }
      this.connectionOptionsFor = client.connID;
      client.connectionOptions().then((opts) => {
        this.connectionOptions = Array.isArray(opts) ? opts : [];
      }).catch(() => {
        // Older backend without /options: keep the card hidden.
        this.connectionOptions = [];
      });
    },
    refreshInit() {
      this.refreshTimer && clearInterval(this.refreshTimer);

      if (this.autoRefresh) {
        this.initShow();

        this.refreshTimer = setInterval(() => {
          this.initShow();
        }, this.refreshInterval);
      }
    },
    sortByKeys(a, b) {
      return a.keys - b.keys;
    },
    sortByExpires(a, b) {
      return a.expires - b.expires;
    },
    sortByTTL(a, b) {
      return a.avg_ttl - b.avg_ttl;
    },
    initStatus(content) {
      if (!content) {
        return {};
      }

      content = content.split('\n');
      const lines = {};

      for (let i of content) {
        i = i.replace(/\s/ig, '');
        if (i.startsWith('#') || !i) continue;

        const kv = i.split(':');
        lines[kv[0]] = kv[1];
      }

      return lines;
    },
    initDbKeys(status, name = undefined) {
      const dbs = [];

      for (const i in status) {
        // fix #1101 unexpected db prefix
        // if (i.startsWith('db')) {
        if (/^db\d+/.test(i)) {
          const array = status[i].split(',');

          const keys = parseInt(array[0] ? array[0].split('=')[1]: NaN);
          const expires = parseInt(array[1] ? array[1].split('=')[1] : NaN);
          const avg_ttl = parseInt(array[2] ? array[2].split('=')[1] : NaN);

          // #1261 locale to the key count
          dbs.push({
            db: i,
            keys,
            expires,
            avg_ttl,
            keys_show: keys.toLocaleString(),
            expires_show: expires.toLocaleString(),
            avg_ttl_show: avg_ttl.toLocaleString(),
            name,
          });
        }
      }

      return dbs;
    },
    initClusterKeys() {
      // The embedded HttpRedisClient proxies a single backend connection, so
      // we cannot open a per-node sub-client to run `INFO KEYSPACE` on each
      // cluster node. Instead we fetch the cluster node topology from the
      // proxy and surface each node's id/addr/role/flags/slots/link-state in
      // the key-statistics table (node column + the other columns).
      this.client.nodes().then((nodes) => {
        if (!nodes || !nodes.length) {
          return;
        }

        const rows = nodes.map((node) => ({
          name: node.addr,
          db: node.role,
          keys: 0,
          expires: 0,
          avg_ttl: 0,
          keys_show: (node.slots && node.slots.length) ? node.slots.slice(0, 3).join(' ') : '-',
          expires_show: node.flags || '-',
          avg_ttl_show: node.linkState || '-',
        }));

        rows.sort((a, b) => (a.name > b.name ? 1 : -1));
        this.DBKeys = rows;
      }).catch((e) => {
        this.$message.error(e.message);
      });
    },
    initShortcut() {
      this.$shortcut.bind('ctrl+r, ⌘+r, f5', this.hotKeyScope, () => {
        this.initShow();
        return false;
      });
    },
  },
  mounted() {
    this.initShow();
    this.refreshInit();
    this.initShortcut();
  },
  beforeDestroy() {
    // clear interval when tab is closed
    clearInterval(this.refreshTimer);
    this.$shortcut.deleteScope(this.hotKeyScope);
  },
};
</script>

<style type="text/css">
  .el-row.status-card {
    margin-top: 20px;
  }
  .server-status-tag-p {
    height: 32px;
  }
  .server-status-container{
    width: 100%;
    overflow-x: hidden;
    text-overflow: ellipsis;
  }
  .server-status-text{
    color: #43b50b;
  }
  .status-filter-input {
    float: right;
    width: 100px;
  }

  /*fix table height changes[scrollTop changes] when tab toggled*/
  .status-card .el-table__header-wrapper{
      height: 50px;
  }
  .status-card .el-table__body-wrapper{
      /*height: calc(100% - 50px) !important;*/
  }
</style>
