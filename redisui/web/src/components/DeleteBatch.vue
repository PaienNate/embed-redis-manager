<template>
<div>
  <el-card class="box-card del-batch-card">
    <!-- card title -->
    <div slot="header" class="clearfix">
      <span class="del-title"><i class="fa fa-exclamation-triangle"></i> {{ $t('message.keys_to_be_deleted') }}</span>
      <i v-if="loadingScan||loadingDelete" class='el-icon-loading'></i>
      <el-tag size="mini">
        <span v-if="loadingScan">Scanning... </span>
        <span v-if="loadingDelete">Deleting... </span>
        Total: {{ allKeysList.length }}
      </el-tag>

      <!-- del btn -->
      <el-button @click="confirmDelete" :disabled="loadingScan||loadingDelete||allKeysList.length == 0" style="float: right;" type="danger">{{ $t('message.delete_all') }}</el-button>
      <!-- toggle scanning btn -->
      <el-button v-if="rule.pattern.length && !scanningEnd" @click="toggleScanning()" type="text" style="float: right;">{{loadingScan ? $t('message.pause') : $t('message.begin')}}&nbsp;</el-button>
    </div>

    <!-- scan pattern -->
    <el-tag v-if="rule.pattern && rule.pattern.length" size="mini" style="margin-left: 10px;">
      <i class="fa fa-search"></i> {{rule.pattern.join(' ')}}
    </el-tag>

    <!-- key list -->
    <RecycleScroller
      class="del-batch-key-list"
      :items="allKeysList"
      :item-size="20"
      key-field="str"
      v-slot="{ item, index }"
    >
      <li>
        <span class="list-index">{{ index + 1 }}.</span>
        <span class="key-name" :title="item.str">{{ item.str }}</span>
      </li>
    </RecycleScroller>
  </el-card>
</div>
</template>

<script type="text/javascript">
import { RecycleScroller } from 'vue-virtual-scroller';
import createScanStream from '@/scanStream';

export default {
  data() {
    return {
      loadingScan: false,
      loadingDelete: false,
      scanStreams: [],
      allKeysList: [],
      scanningEnd: false,
    };
  },
  props: ['client', 'rule', 'hotKeyScope'],
  components: { RecycleScroller },
  methods: {
    initKeys() {
      this.allKeysList = [];
      this.rule.key && this.rule.key.length && this.addToList(this.rule.key);

      if (this.rule.pattern && this.rule.pattern.length) {
        this.loadingScan = true;

        for (const pattern of this.rule.pattern) {
          this.initScanStreamsAndScan(pattern);
        }
      }
    },
    initScanStreamsAndScan(pattern) {
      // The embedded client is a single proxy connection; its scanBufferStream
      // is an async iterable that createScanStream re-shapes into the
      // pause/resume + on('data') interface used here.
      const scanOption = {
        match: `${pattern}*`,
        count: 20000,
      };

      const stream = createScanStream(this.client.scanBufferStream(scanOption), 20000);
      this.scanStreams.push(stream);
      this.scanningCount = 1;

      stream.on('data', (keys) => {
        this.addToList(keys.sort());

        // pause for dom rendering
        stream.pause();
        setTimeout(() => {
          this.loadingScan && stream.resume();
        }, 100);
      });

      stream.on('error', (e) => {
        this.loadingScan = false;
        this.$message.error({
          message: `Delete Batch Stream On Error: ${e.message}`,
          duration: 1500,
        });
      });

      stream.on('end', () => {
        this.loadingScan = false;
        this.scanningEnd = true;
      });
    },
    addToList(keys) {
      const list = [];
      for (const key of keys) {
        list.push({ key, str: this.$util.bufToString(key) });
      }

      this.allKeysList = this.allKeysList.concat(list);
    },
    toggleScanning(forcePause = null) {
      this.loadingScan = (forcePause === null ? !this.loadingScan : !forcePause);

      if (this.scanStreams.length) {
        for (const stream of this.scanStreams) {
          this.loadingScan ? stream.resume() : stream.pause();
        }
      }
    },
    // Detect a real cluster connection. HttpRedisClient.nodes() is async and
    // resolves to [] for a standalone connection (non-empty node list only
    // for a cluster), mirroring KeyList's getScanNodes fallback logic.
    async isClusterClient() {
      if (!this.client || typeof this.client.nodes !== 'function') {
        return false;
      }
      try {
        const nodes = await this.client.nodes('master');
        return Array.isArray(nodes) && nodes.length > 0;
      } catch (e) {
        return false;
      }
    },
    async confirmDelete() {
      const keys = this.allKeysList.map((k) => k.key);
      const total = keys.length;

      if (total <= 0) {
        return;
      }

      this.loadingDelete = true;

      try {
        const isCluster = await this.isClusterClient();
        // standalone: batch up to 5000 keys per DEL; cluster: one key per
        // DEL (a multi-key DEL across slots fails with CROSSSLOT).
        const chunkSize = isCluster ? 1 : 5000;
        const chunks = [];
        for (let i = 0; i < total; i += chunkSize) {
          chunks.push(keys.slice(i, i + chunkSize));
        }

        // Wait for each wave of chunks instead of firing every request at
        // once: at most 8 DEL requests are in flight, so a huge key list
        // cannot turn into an unbounded burst of HTTP requests.
        const MAX_CONCURRENT = 8;
        let next = 0;
        let deleted = 0;
        let aborted = false;
        const worker = async () => {
          while (!aborted && next < chunks.length) {
            const chunk = chunks[next++];
            try {
              const reply = await this.client.del(chunk);
              deleted += Number(reply) || 0;
            } catch (e) {
              // Stop the other workers from consuming more chunks; in-flight
              // requests finish but no new ones start after a failure.
              aborted = true;
              throw e;
            }
          }
        };
        await Promise.all(
          Array.from({ length: Math.min(MAX_CONCURRENT, chunks.length) }, () => worker()),
        );

        if (deleted > 0) {
          this.afterDelete();
        } else {
          this.deleteFailed(this.$t('message.delete_failed'));
        }
      } catch (e) {
        this.deleteFailed(e.message);
      }
    },
    afterDelete() {
      this.loadingDelete = false;
      this.allKeysList = [];

      // empty the specified keys
      // this.rule.key = [];

      this.$message.success(this.$t('message.delete_success'));
      this.$bus.$emit('refreshKeyList', this.client);

      // except pattern mode scanning not to end, close pre tab
      if (!this.rule.pattern.length || this.scanningEnd) {
        this.$bus.$emit('removePreTab');
      }
    },
    deleteFailed(msg = '') {
      msg && this.$message.error(msg);

      this.loadingScan = false;
      this.loadingDelete = false;
    },
    initShortcut() {
      this.$shortcut.bind('ctrl+r, ⌘+r, f5', this.hotKeyScope, () => {
        this.initKeys();
        return false;
      });
    },
  },
  mounted() {
    this.initKeys();
    // disable f5 for streams on event cannot stop
    // this.initShortcut();
  },
  beforeDestroy() {
    // this.$shortcut.deleteScope(this.hotKeyScope);
    // cancel scanning
    this.toggleScanning(true);
  },
};
</script>

<style type="text/css" >
  .del-title {
    color: #f56c6c;
    font-weight: bold;
    font-size: 120%;
  }
  .del-batch-card {
    /*margin-top: 10px;*/
  }
  .del-batch-key-list {
    height: calc(100vh - 204px);
    overflow: auto;
    padding-left: 10px;
    list-style: none;
    margin-top: 10px;
  }
  .del-batch-key-list li {
    color: #333;
    font-size: 92%;
    display: flex;
  }
  .dark-mode .del-batch-key-list li {
    color: #f7f7f7;
  }
  .del-batch-key-list li .key-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
