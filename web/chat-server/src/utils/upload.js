// upload.js 文件/头像上传统一入口。
// 后端上传接口返回落盘后的相对路径（如 /static/files/<新文件名>），
// 前端拿返回值拼全 URL，不再用本地文件名猜测路径（否则同名文件会互相覆盖、
// 且与后端随机重命名的真实文件名不一致）。
import axios from "axios";
import store from "../store";

export async function uploadFile(apiPath, file) {
  const formData = new FormData();
  formData.append("file", file);
  // axios 对 FormData 会自动带 boundary；不要手动设 Content-Type，
  // 否则后端 multipart 解析不到文件。鉴权头由全局请求拦截器附带。
  const rsp = await axios.post(store.state.apiUrl + apiPath, formData);
  if (rsp.data && rsp.data.code === 0) {
    return rsp.data.data; // 相对路径
  }
  throw new Error((rsp.data && rsp.data.message) || "上传失败");
}

export default { uploadFile };