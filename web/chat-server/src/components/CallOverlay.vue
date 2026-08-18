<template>
  <div v-if="phase !== 'idle'" class="call-overlay">
    <div class="call-card">
      <div class="call-header">
        <img :src="peer.avatar" class="call-avatar" />
        <div class="call-title">
          <h3>{{ peer.name }}</h3>
          <p class="call-status">{{ statusText }}</p>
        </div>
      </div>
      <div v-if="phase === 'incall'" class="call-videos">
        <video
          ref="localVideoRef"
          autoplay
          playsinline
          muted
          class="call-video call-video-local"
        ></video>
        <video
          ref="remoteVideoRef"
          autoplay
          playsinline
          class="call-video call-video-remote"
        ></video>
      </div>
      <div class="call-actions">
        <template v-if="phase === 'incoming'">
          <el-button type="success" round @click="acceptCall">接听</el-button>
          <el-button type="danger" round @click="rejectIncoming">拒绝</el-button>
        </template>
        <template v-else>
          <el-button type="danger" round @click="hangUp">挂断</el-button>
        </template>
      </div>
    </div>
  </div>
</template>

<script>
import { computed, nextTick, reactive, ref, toRefs, onBeforeUnmount } from "vue";
import { useStore } from "vuex";
import { ElMessage, ElNotification } from "element-plus";
import axios from "axios";
import { on } from "@/utils/messageBus";
import { sendRaw } from "@/utils/ws";
import { updateTitle } from "@/utils/notify";

const CALL_TIMEOUT_MS = 45 * 1000
const ICE_CFG = {
  iceServers: [{ urls: ["stun:stun.l.google.com:19302"] }],
}

export default {
  name: "CallOverlay",
  setup() {
    const store = useStore();
    const data = reactive({
      phase: "idle", // idle | outgoing | incoming | incall
      peer: { id: "", name: "", avatar: "", sessionId: "" },
      rtcPeerConn: null,
      localStream: null,
      remoteStream: null,
      noAnswerTimer: null,
      ringTimer: null,
      titleFlashTimer: null,
    });
    const localVideoRef = ref(null);
    const remoteVideoRef = ref(null);

    const statusText = computed(() => {
      if (data.phase === "outgoing") return "等待对方接听…";
      if (data.phase === "incoming") return "邀请你音视频通话…";
      if (data.phase === "incall") return "通话中";
      return "";
    });

    // ---------- 信令收发 ----------

    // sendSignalTo 面向任意对端发送信令（不打断进行中的通话时用它，peer 显式传入）
    const sendSignalTo = (peer, avData) => {
      const rtcMessageRequest = {
        session_id: peer.sessionId,
        type: 3,
        content: "",
        url: "",
        send_id: store.state.userInfo.uuid,
        send_name: store.state.userInfo.nickname,
        send_avatar: store.state.userInfo.avatar,
        receive_id: peer.id,
        file_size: "",
        file_name: "",
        file_type: "",
        av_data: JSON.stringify(avData),
      };
      if (!sendRaw(store, rtcMessageRequest)) {
        ElMessage.error("连接已断开，信令发送失败");
        return false;
      }
      return true;
    };

    const sendSignal = (avData) => sendSignalTo(data.peer, avData);

    const openSession = async (peerId) => {
      try {
        const rsp = await axios.post(store.state.apiUrl + "/session/openSession", {
          send_id: store.state.userInfo.uuid,
          receive_id: peerId,
        });
        data.peer.sessionId = rsp.data.data;
      } catch (error) {
        console.error(error);
      }
    };

    // ---------- WebRTC ----------

    const createRtcPeerConnection = () => {
      if (data.rtcPeerConn) {
        return;
      }
      data.rtcPeerConn = new RTCPeerConnection(ICE_CFG);
      data.rtcPeerConn.onicecandidate = (event) => {
        if (event.candidate) {
          sendSignal({
            messageId: "PROXY",
            type: "candidate",
            messageData: { candidate: event.candidate },
          });
        }
      };
      data.rtcPeerConn.ontrack = (event) => {
        if (!data.remoteStream) {
          data.remoteStream = new MediaStream();
          if (remoteVideoRef.value) {
            remoteVideoRef.value.srcObject = data.remoteStream;
          }
        }
        data.remoteStream.addTrack(event.track);
      };
    };

    const getLocalMediaStream = async () => {
      if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        ElMessage.error("当前浏览器不支持音视频通话");
        return null;
      }
      if (data.localStream) {
        return data.localStream;
      }
      try {
        data.localStream = await navigator.mediaDevices.getUserMedia({
          video: true,
          audio: true,
        });
      } catch (error) {
        console.error(error);
        ElMessage.error("获取摄像头/麦克风失败：" + (error.message || error.name));
        return null;
      }
      data.localStream.getTracks().forEach((track) => {
        data.rtcPeerConn.addTrack(track);
      });
      await attachVideosAfterRender();
      return data.localStream;
    };

    // 视频节点只在 incall 阶段渲染，进入通话后统一在这里挂流
    const attachVideosAfterRender = async () => {
      await nextTick();
      if (localVideoRef.value && data.localStream) {
        localVideoRef.value.srcObject = data.localStream;
      }
      if (remoteVideoRef.value && data.remoteStream) {
        remoteVideoRef.value.srcObject = data.remoteStream;
      }
    };

    const createOffer = () => {
      data.rtcPeerConn
        .createOffer({ offerToReceiveAudio: true, offerToReceiveVideo: true })
        .then((desc) => {
          data.rtcPeerConn.setLocalDescription(desc);
          sendSignal({ messageId: "PROXY", type: "sdp", messageData: { sdp: desc } });
        })
        .catch((err) => console.error("createOffer failed:", err));
    };

    const createAnswer = () => {
      data.rtcPeerConn
        .createAnswer()
        .then((desc) => {
          data.rtcPeerConn.setLocalDescription(desc);
          sendSignal({ messageId: "PROXY", type: "sdp", messageData: { sdp: desc } });
        })
        .catch((err) => console.error("createAnswer failed:", err));
    };

    // ---------- 提示音 / 标题闪烁 ----------

    const startRingtone = () => {
      let ctx = null;
      try {
        const Ctx = window.AudioContext || window.webkitAudioContext;
        if (!Ctx) return;
        ctx = new Ctx();
      } catch (e) {
        return;
      }
      data.ringCtx = ctx;
      const beep = () => {
        try {
          const osc = ctx.createOscillator();
          const gain = ctx.createGain();
          osc.frequency.value = 880;
          gain.gain.value = 0.06;
          osc.connect(gain);
          gain.connect(ctx.destination);
          osc.start();
          osc.stop(ctx.currentTime + 0.12);
        } catch (e) {
          // ignore
        }
      };
      beep();
      data.ringTimer = setInterval(beep, 1500);
      // 30 秒后停止响铃（来电卡片仍在）
      setTimeout(() => {
        if (data.ringTimer && data.phase === "incoming") {
          stopRingtone();
        }
      }, 30 * 1000);
    };

    const stopRingtone = () => {
      if (data.ringTimer) {
        clearInterval(data.ringTimer);
        data.ringTimer = null;
      }
      if (data.ringCtx) {
        try {
          data.ringCtx.close();
        } catch (e) {
          // ignore
        }
        data.ringCtx = null;
      }
    };

    const startTitleFlash = () => {
      let on = false;
      data.titleFlashTimer = setInterval(() => {
        document.title = on ? "GoChat" : "【来电】有新的通话请求";
        on = !on;
      }, 800);
    };

    const stopTitleFlash = () => {
      if (data.titleFlashTimer) {
        clearInterval(data.titleFlashTimer);
        data.titleFlashTimer = null;
      }
      updateTitle();
    };

    // ---------- 状态清理 ----------

    const teardown = (message) => {
      stopRingtone();
      stopTitleFlash();
      if (data.noAnswerTimer) {
        clearTimeout(data.noAnswerTimer);
        data.noAnswerTimer = null;
      }
      if (data.localStream) {
        data.localStream.getTracks().forEach((track) => track.stop());
        data.localStream = null;
      }
      data.remoteStream = null;
      if (data.rtcPeerConn) {
        try {
          data.rtcPeerConn.close();
        } catch (e) {
          // ignore
        }
        data.rtcPeerConn = null;
      }
      data.phase = "idle";
      data.peer = { id: "", name: "", avatar: "", sessionId: "" };
      if (message) {
        ElMessage.warning(message);
      }
    };

    // ---------- 通话动作 ----------

    const startOutgoing = async (payload) => {
      if (!payload || !payload.peer || !payload.peer.id || payload.peer.id[0] !== "U") {
        return;
      }
      if (data.phase !== "idle") {
        ElMessage.warning("已在通话中");
        return;
      }
      data.peer = { ...payload.peer };
      createRtcPeerConnection();
      const stream = await getLocalMediaStream();
      if (!stream) {
        teardown();
        return;
      }
      data.phase = "outgoing";
      if (!sendSignal({ messageId: "PROXY", type: "start_call" })) {
        teardown();
        return;
      }
      data.noAnswerTimer = setTimeout(() => {
        sendSignal({ messageId: "PEER_LEAVE" });
        teardown("对方无应答");
      }, CALL_TIMEOUT_MS);
    };

    const acceptCall = async () => {
      stopRingtone();
      stopTitleFlash();
      if (data.noAnswerTimer) {
        clearTimeout(data.noAnswerTimer);
        data.noAnswerTimer = null;
      }
      await openSession(data.peer.id);
      // 先切到通话态让视频节点渲染，再取媒体并挂流
      data.phase = "incall";
      createRtcPeerConnection();
      const stream = await getLocalMediaStream();
      if (!stream) {
        sendSignal({ messageId: "PROXY", type: "reject_call" });
        teardown();
        return;
      }
      sendSignal({ messageId: "PROXY", type: "receive_call" });
    };

    const rejectIncoming = () => {
      stopRingtone();
      stopTitleFlash();
      if (data.noAnswerTimer) {
        clearTimeout(data.noAnswerTimer);
        data.noAnswerTimer = null;
      }
      const peerId = data.peer.id;
      const hadSession = !!data.peer.sessionId;
      // reject_call 需要信封里的 session_id，被叫可能还没开过会话
      if (!hadSession) {
        openSession(peerId).then(() => {
          sendSignal({ messageId: "PROXY", type: "reject_call" });
          teardown();
        });
      } else {
        sendSignal({ messageId: "PROXY", type: "reject_call" });
        teardown();
      }
    };

    const hangUp = () => {
      sendSignal({ messageId: "PEER_LEAVE" });
      teardown("通话已结束");
    };

    // ---------- 下行信令处理 ----------

    const handleAvSignal = async (message) => {
      let av
      try {
        av = JSON.parse(message.av_data)
      } catch (e) {
        console.warn('[call] 无法解析 av_data', e)
        return
      }
      // 多人聊天室信令（CURRENT_PEERS/PEER_JOIN）当前不支持，仅处理 1v1
      if (av.messageId === 'PEER_LEAVE') {
        if (data.phase !== 'idle') {
          teardown('对方已挂断')
        }
        return
      }
      // start_call 单独前置处理：忙线时需要礼貌回拒且不能影响进行中的通话
      if (av.type === 'start_call') {
        if (data.phase !== 'idle') {
          try {
            const rsp = await axios.post(
              store.state.apiUrl + '/session/openSession',
              { send_id: store.state.userInfo.uuid, receive_id: message.send_id }
            )
            sendSignalTo(
              { id: message.send_id, sessionId: rsp.data.data },
              { messageId: 'PROXY', type: 'reject_call' }
            )
          } catch (e) {
            console.error(e)
          }
          return
        }
        data.peer = {
          id: message.send_id,
          name: message.send_name || '未知用户',
          avatar: message.send_avatar || '',
          sessionId: '',
        }
        data.phase = 'incoming'
        startRingtone()
        startTitleFlash()
        ElNotification({
          title: '来电提醒',
          message: `${data.peer.name} 邀请你音视频通话`,
          type: 'warning',
          duration: 0,
        })
        data.noAnswerTimer = setTimeout(() => {
          // 45 秒未接听自动拒绝
          stopRingtone()
          stopTitleFlash()
          if (data.phase === 'incoming') {
            const peerId = data.peer.id
            openSession(peerId).then(() => {
              sendSignal({ messageId: 'PROXY', type: 'reject_call' })
              teardown('未接听')
            })
          }
        }, CALL_TIMEOUT_MS)
        return
      }
      if (av.messageId !== 'PROXY') {
        return
      }
      // 与当前通话无关的信令（比如另一路来电的中间态）忽略
      if (data.phase !== 'idle' && data.peer.id && message.send_id !== data.peer.id) {
        return
      }
      if (av.type === 'receive_call') {
        if (data.phase !== 'outgoing') return
        if (data.noAnswerTimer) {
          clearTimeout(data.noAnswerTimer)
          data.noAnswerTimer = null
        }
        data.phase = 'incall'
        attachVideosAfterRender()
        createOffer()
        return
      }
      if (av.type === 'reject_call') {
        if (data.phase === 'outgoing' || data.phase === 'incall') {
          if (data.noAnswerTimer) {
            clearTimeout(data.noAnswerTimer)
            data.noAnswerTimer = null
          }
          teardown('对方已拒绝')
        }
        return
      }
      if (av.type === 'sdp') {
        const sdp = av.messageData && av.messageData.sdp
        if (!sdp || !data.rtcPeerConn) return
        if (sdp.type === 'offer') {
          data.rtcPeerConn
            .setRemoteDescription(new RTCSessionDescription(sdp))
            .then(() => createAnswer())
            .catch((err) => console.error('setRemoteDescription failed', err))
        } else if (sdp.type === 'answer') {
          data.rtcPeerConn
            .setRemoteDescription(new RTCSessionDescription(sdp))
            .catch((err) => console.error('setRemoteDescription failed', err))
        }
        return
      }
      if (av.type === 'candidate') {
        const candidate = av.messageData && av.messageData.candidate
        if (candidate && data.rtcPeerConn) {
          data.rtcPeerConn
            .addIceCandidate(new RTCIceCandidate(candidate))
            .catch((err) => console.error('addIceCandidate failed', err))
        }
      }
    };

    // 通话期间断线重连：信令通道已不可信，直接结束通话
    on('ws:connected', () => {
      if (data.phase === 'incall' || data.phase === 'outgoing') {
        teardown('连接中断，通话已结束')
      }
    });
    on('av-signal', handleAvSignal);
    on('call:start', startOutgoing);

    onBeforeUnmount(() => {
      if (data.phase !== 'idle') {
        teardown()
      }
    });

    return {
      ...toRefs(data),
      localVideoRef,
      remoteVideoRef,
      statusText,
      acceptCall,
      rejectIncoming,
      hangUp,
    };
  },
};
</script>

<style scoped>
.call-overlay {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 2100;
}

.call-card {
  width: 320px;
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  border-radius: 20px;
  border: 1px solid var(--go-border);
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.98) 0%, rgba(246, 249, 246, 0.98) 100%);
  box-shadow: 0 18px 42px rgba(16, 22, 18, 0.18);
}

.call-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.call-avatar {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  object-fit: cover;
}

.call-title {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.call-title h3 {
  margin: 0;
  color: var(--go-text-strong);
  font-size: 16px;
  font-weight: 600;
}

.call-status {
  margin: 0;
  color: var(--go-text-muted);
  font-size: 12px;
}

.call-videos {
  display: flex;
  gap: 10px;
}

.call-video {
  width: 138px;
  height: 104px;
  border-radius: 12px;
  border: 1px solid var(--go-border);
  background: #111;
  object-fit: cover;
}

.call-actions {
  display: flex;
  justify-content: center;
  gap: 10px;
}
</style>
