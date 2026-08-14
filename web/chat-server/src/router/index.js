import { createRouter, createWebHistory } from "vue-router";
import store from "../store/index.js";
import Login from "../views/access/Login.vue";
import SmsLogin from "../views/access/SmsLogin.vue";
import Register from "../views/access/Register.vue";
import ChatLayout from "../views/chat/ChatLayout.vue";
import OwnInfo from "../views/chat/user/OwnInfo.vue";
import ContactList from "../views/chat/contact/ContactList.vue";
import ContactChat from "../views/chat/contact/ContactChat.vue";
import SessionList from "../views/chat/session/SessionList.vue";
import Manager from "../views/manager/Manager.vue";

const routes = [
  {
    path: "/",
    redirect: { name: "Login" },
  },
  {
    path: "/login",
    name: "Login",
    component: Login,
  },
  {
    path: "/smsLogin",
    name: "smsLogin",
    component: SmsLogin,
  },
  {
    path: "/register",
    name: "Register",
    component: Register,
  },
  {
    path: "/chat",
    component: ChatLayout,
    redirect: { name: "SessionList" },
    children: [
      {
        path: "owninfo",
        name: "OwnInfo",
        component: OwnInfo,
      },
      {
        path: "contactlist",
        name: "ContactList",
        component: ContactList,
      },
      {
        path: "sessionlist",
        name: "SessionList",
        component: SessionList,
      },
      {
        path: ":id",
        name: "ContactChat",
        component: ContactChat,
      },
    ],
  },
  {
    path: "/manager",
    name: "Manager",
    component: Manager,
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

router.beforeEach((to, from, next) => {
  if (!store.getters.isLoggedIn) {
    if (
      to.path === "/login" ||
      to.path === "/register" ||
      to.path === "/smsLogin"
    ) {
      next();
      return;
    }
    next("/login");
  } else {
    next();
  }
});

export default router;
