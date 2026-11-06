<template>
  <div class="cli-content-container">
    <!-- code editor div -->
    <div class="code-editor-con" ref="editor"></div>
  </div>
</template>

<script type="text/javascript">
import { createEditor, destroyEditor } from '@/lib/codeEditor';

export default {
  props: {
    content: { type: String, default: () => {} },
  },
  created() {
    // listen font family change and re-render so the metrics stay aligned
    this.$bus.$on('fontInited', this.changeFont);
  },
  watch: {
    // refresh
    content(newVal) {
      this.codeEditor && this.codeEditor.setValue(newVal);
    },
  },
  methods: {
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
    scrollToBottom() {
      if (!this.codeEditor) {
        return;
      }
      this.codeEditor.scrollTo(0, this.codeEditor.getDoc().height);
    },
  },

  mounted() {
    this.codeEditor = createEditor(this.$refs.editor, {
      value: this.content,
      json: false,
      readOnly: true,
      theme: 'material-darker',
    });

    this.$nextTick(() => this.codeEditor && this.codeEditor.refresh());
    window.addEventListener('resize', this.onResize);
  },
  beforeDestroy() {
    window.removeEventListener('resize', this.onResize);

    if (this.codeEditor) {
      destroyEditor(this.codeEditor);
      this.codeEditor = null;
    }

    this.$bus.$off('fontInited', this.changeFont);
  },
};
</script>

<style type="text/css">
  .cli-content-container .code-editor-con {
    min-height: 150px;
    height: calc(100vh - 123px);
    clear: both;
    overflow: hidden;
    background: #263238;
    border: 1px solid #e4e7ed;
    border-bottom: 0px;
    border-radius: 4px 4px 0 0;
  }
  .dark-mode .cli-content-container .code-editor-con {
    background: #324148;
    border-color: #7f8ea5;
  }
  .cli-content-container .code-editor-con .CodeMirror {
    height: 100%;
    background: transparent;
    font-size: 14px;
    /* Pin line-height to the font size so the caret isn't oversized. */
    line-height: 20px;
  }
  .cli-content-container .code-editor-con .CodeMirror pre.CodeMirror-line,
  .cli-content-container .code-editor-con .CodeMirror pre.CodeMirror-line-like {
    font-size: inherit;
    line-height: inherit;
    font-family: inherit;
  }
</style>
