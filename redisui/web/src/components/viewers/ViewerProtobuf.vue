<template>
  <JsonEditor ref='editor' :content='newContent' :readOnly='false' class='protobuf-viewer'>
    <div class="viewer-protobuf-header">
      <!-- type selector -->
      <el-select v-model="selectedType" filterable placeholder="Select Type" size="mini" class="type-selector">
        <el-option
          v-for="t of types"
          :key="t"
          :label="t"
          :value="t">
        </el-option>
      </el-select>
      <!-- select proto file -->
      <el-button
        class="select-proto-btn"
        type='primary'
        size="mini"
        icon="el-icon-upload2"
        :title="buttonTitle"
        @click="selectProto">
        <span class="select-proto-text">{{ buttonText }}</span>
        <!-- clear proto files -->
        <i
          v-if="proto.length"
          class="el-icon-close"
          title="Clear Proto"
          @click.stop="clearProto">
        </i>
      </el-button>
    </div>
    <hr>
  </JsonEditor>
</template>

<script type="text/javascript">
import JsonEditor from '@/components/JsonEditor';
import storage from '@/storage';
import { getData } from 'rawproto';
import protobuf from 'protobufjs';

const DECODE_FAILED = 'Protobuf Decode Failed!';

function basename(p = '') {
  const parts = String(p).split(/[\\/]/);
  return parts[parts.length - 1] || '';
}

export default {
  data() {
    return {
      proto: [],
      bookmarks: [],
      protoRoot: null,
      types: ['Rawproto'],
      selectedType: 'Rawproto',
    };
  },
  components: { JsonEditor },
  props: ['content', 'redisKey'],
  computed: {
    newContent() {
      try {
        if (this.selectedType === 'Rawproto') {
          return getData(this.content);
        }
        const type = this.protoRoot.lookupType(this.selectedType);
        const message = type.decode(this.content);
        // notice: toJSON will convert int64 to string
        // can use toObject instead: type.toObject(message, {longs: BigInt})
        return message.toJSON();
      } catch (e) {
        return DECODE_FAILED;
      }
    },
    buttonTitle() {
      return this.proto.length ? this.proto.join('\n') : 'Select Proto Files';
    },
    buttonText() {
      if (!this.proto.length) {
        return 'Select Proto Files';
      }

      return this.proto.map(p => basename(p)).join(', ');
    },
    keyHex() {
      return this.redisKey.toString('hex');
    },
  },
  mounted() {
    this.restoreBinding();
  },
  methods: {
    getContent() {
      if (!this.protoRoot) {
        this.$message.error('Select a correct .proto file');
        return false;
      }

      if (!this.selectedType || this.selectedType === 'Rawproto') {
        this.$message.error('Select a correct Type to encode');
        return false;
      }

      let content = this.$refs.editor.getRawContent();
      const type = this.protoRoot.lookupType(this.selectedType);

      try {
        content = JSON.parse(content);
      } catch (e) {
        this.$message.error(this.$t('message.json_format_failed'));
        return false;
      }

      // toJSON() shows int64/uint64 as strings; fromObject converts them back
      const message = type.fromObject(content);
      const err = type.verify(message);
      if (err) {
        this.$message.error(`Proto Verify Failed: ${err}`);
        return false;
      }

      return type.encode(message).finish();
    },
    copyContent() {
      return JSON.stringify(this.newContent);
    },
    selectProto() {
      const input = document.createElement('input');
      input.type = 'file';
      input.accept = '.proto';
      input.multiple = true;
      input.addEventListener('change', () => {
        const files = Array.from(input.files || []);
        if (!files.length) {
          return;
        }
        Promise.all(files.map(file => new Promise((resolve) => {
          const reader = new FileReader();
          reader.onload = event => resolve({ name: file.name, content: event.target.result });
          reader.readAsText(file);
        }))).then((sources) => {
          this.loadProtoSources(sources, true);
        });
      });
      input.click();
    },
    restoreBinding() {
      const profile = storage.getProtobufProfileByRedisKey(this.keyHex);
      // Browser build cannot re-read .proto file contents from disk, so binding
      // is only kept for display purposes; source is not restored automatically.
    },
    loadProtoSources(sources, persist = false) {
      if (!sources.length) {
        return Promise.resolve(false);
      }

      try {
        const root = protobuf.parse(sources.map(s => s.content).join('\n')).root;
        this.proto = sources.map(s => s.name);
        this.protoRoot = root;
        this.types = ['Rawproto'];
        this.traverseTypes(root);
        // default select the first real type
        this.selectedType = this.types.length > 1 ? this.types[1] : 'Rawproto';

        if (persist) {
          this.persistBinding();
        }
        return true;
      } catch (e) {
        this.$message.error(`Fail to load proto: ${e.message}`);
        this.clearBinding();
        return false;
      }
    },
    persistBinding() {
      const redisKeyHex = this.keyHex;
      if (!redisKeyHex || !this.proto.length) {
        return;
      }

      storage.bindProtobufProfile(redisKeyHex, {
        name: this.proto.map(p => basename(p)).join(', '),
        paths: this.proto,
      });
    },
    clearBinding() {
      storage.unbindProtobufProfile(this.keyHex);
    },
    clearProto() {
      this.clearBinding();
      this.proto = [];
      this.bookmarks = [];
      this.protoRoot = null;
      this.types = ['Rawproto'];
      this.selectedType = 'Rawproto';
    },
    traverseTypes(current) {
      if (current instanceof protobuf.Type) {
        this.types.push(current.fullName);
      }
      if (current.nestedArray) {
        current.nestedArray.forEach((nested) => {
          this.traverseTypes(nested);
        });
      }
    },
  },
};
</script>

<style type="text/css">
  .viewer-protobuf-header {
    display: flex;
    margin-top: 8px;
  }
  .viewer-protobuf-header .type-selector {
    flex: 1;
    margin-right: 10px;
  }
  .viewer-protobuf-header .select-proto-btn {
    margin-top: 2px;
    height: 27px;
    max-width: 240px;
    padding-right: 8px;
  }
  .viewer-protobuf-header .select-proto-btn .select-proto-text {
    display: inline-block;
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: bottom;
  }

  /*text viewer box*/
  .key-content-string .text-formated-container.protobuf-viewer .code-editor-con {
    height: calc(100vh - 331px);
  }
</style>
