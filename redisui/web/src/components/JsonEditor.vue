<template>
  <div class="text-formated-container">
    <slot name='default'></slot>
    <!-- collapse btn -->
    <div class="collapse-container">
      <el-button class="collapse-btn"  type="text" @click="toggleCollapse">{{ $t('message.' + collapseText) }}</el-button>
    </div>

    <!-- code editor div -->
    <div class="code-editor-con" ref="editor"></div>
  </div>
</template>

<script type="text/javascript">
import { createEditor } from '@/lib/codeEditor';
import JSONbigFactory from '@qii404/json-bigint';

const JSONbig = JSONbigFactory({ useNativeBigInt: false });

export default {
  data() {
    return {
      collapseText: 'collapse_all',
    };
  },
  props: {
    content: { type: Array | String, default: () => {} },
    readOnly: { type: Boolean, default: true },
  },
  created() {
    // listen font family change and re-render so the metrics stay aligned
    this.$bus.$on('fontInited', this.changeFont);
  },
  computed: {
    newContentStr() {
      if (typeof this.content === 'string') {
        return this.content;
      }

      return JSONbig.stringify(this.content, null, 4);
    },
  },
  watch: {
    // refresh
    content() {
      this.$nextTick(() => {
        this.codeEditor && this.codeEditor.setValue(this.newContentStr);
      });
    },
  },
  methods: {
    getContent() {
      const content = this.codeEditor.getValue();

      if (!this.$util.isJson(content)) {
        this.$message.error(this.$t('message.json_format_failed'));
        return false;
      }

      return Buffer.from(JSONbig.stringify(JSONbig.parse(content), null, 0));
    },
    getRawContent(removeJsonSpace = false) {
      let content = this.codeEditor.getValue();

      if (removeJsonSpace) {
        if (this.$util.isJson(content)) {
          content = JSONbig.stringify(JSONbig.parse(content), null, 0);
        }
      }

      return content;
    },
    toggleCollapse() {
      this.codeEditor.execCommand(this.collapseText === 'expand_all' ? 'unfoldAll' : 'foldAll');
      this.collapseText = this.collapseText === 'expand_all' ? 'collapse_all' : 'expand_all';
    },
    onResize() {
      if (!this.resizeDebounce) {
        this.resizeDebounce = this.$util.debounce(() => {
          this.codeEditor && this.codeEditor.refresh();
        }, 200);
      }

      this.resizeDebounce();
    },
    changeFont(fontFamily) {
      if (!this.codeEditor) {
        return;
      }
      this.codeEditor.getWrapperElement().style.fontFamily = fontFamily;
      this.codeEditor.refresh();
    },
  },

  mounted() {
    this.codeEditor = createEditor(this.$refs.editor, {
      value: this.newContentStr,
      json: true,
      readOnly: this.readOnly,
    });

    window.addEventListener('resize', this.onResize);
  },
  beforeDestroy() {
    window.removeEventListener('resize', this.onResize);

    if (this.codeEditor) {
      const wrapper = this.codeEditor.getWrapperElement();
      wrapper && wrapper.parentNode && wrapper.parentNode.removeChild(wrapper);
      this.codeEditor = null;
    }

    this.$bus.$off('fontInited', this.changeFont);
  },
};
</script>

<style type="text/css">
  .text-formated-container .code-editor-con {
    min-height: 150px;
    height: calc(100vh - 730px);
    clear: both;
    overflow: hidden;
    background: none;
  }
  .text-formated-container .code-editor-con .CodeMirror {
    height: 100%;
    background: transparent;
    font-size: 14px;
  }
  .dark-mode .text-formated-container .code-editor-con .CodeMirror {
    background: #273239;
    color: #e6e6e6;
  }

  /*change default scrollbar style*/
  .text-formated-container .code-editor-con .CodeMirror-vscrollbar,
  .text-formated-container .code-editor-con .CodeMirror-hscrollbar {
    background: #eaeaea;
    border-radius: 4px;
  }
  .dark-mode .text-formated-container .code-editor-con .CodeMirror-vscrollbar,
  .dark-mode .text-formated-container .code-editor-con .CodeMirror-hscrollbar {
    background: #425057;
  }
</style>
